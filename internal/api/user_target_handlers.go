package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"proxyma/internal/ids"
	"proxyma/internal/remoteexec"
	"proxyma/internal/sshclient"
	"proxyma/internal/targets"
	"proxyma/internal/tasks"
	"proxyma/internal/users"
	"proxyma/internal/winrmclient"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (s *Server) handleUserTargets(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleCreateUserTarget(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user, _, ok := userSessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
		return
	}
	items := s.targetRegistry().All()
	if !user.Admin {
		owned := make([]targets.Target, 0, len(items))
		for _, target := range items {
			if containsUserID(target.UserIDs, user.ID) {
				owned = append(owned, target)
			}
		}
		items = owned
	}
	writeJSON(w, http.StatusOK, userTargetsResponse(items))
}

func (s *Server) handleTestUserTargetCredentials(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user, _, ok := userSessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
		return
	}
	request, ok := decodeTargetMutationRequest(w, r)
	if !ok {
		return
	}
	if err := validateTargetReferenceIDs(request); err != nil {
		writeTargetMutationError(w, err)
		return
	}
	if request.TargetID != "" {
		var target targets.Target
		found := false
		for _, candidate := range s.targetRegistry().All() {
			if candidate.ID == strings.TrimSpace(request.TargetID) {
				target, found = candidate, true
				break
			}
		}
		if !found {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "target not found", Code: "not_found"})
			return
		}
		if !user.Admin && !containsUserID(target.UserIDs, user.ID) {
			writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "target is not owned by the current user", Code: "forbidden"})
			return
		}
	}
	transport := strings.TrimSpace(request.Transport)
	host := strings.TrimSpace(request.Hostname)
	if host == "" {
		host = strings.TrimSpace(request.IP)
	}
	winrmScheme := ""
	winrmPort := 0
	winrmTLSInsecure := false
	if request.WinRM != nil {
		winrmScheme = strings.ToLower(strings.TrimSpace(request.WinRM.Scheme))
		winrmPort = request.WinRM.Port
		winrmTLSInsecure = request.WinRM.TLSInsecure
	}
	s.debugf("debug target credential test requested user_id=%s transport=%s host=%s winrm_scheme=%s winrm_port=%d winrm_tls_insecure=%t",
		user.ID, transport, host, winrmScheme, winrmPort, winrmTLSInsecure)
	var cleanup func()
	var err error
	var targetThrottleKey string
	var run func() remoteexec.Result
	var label string
	switch transport {
	case targets.TransportSSH:
		var sshRequest sshclient.Request
		sshRequest, cleanup, err = s.credentialTestSSHRequest(request)
		targetThrottleKey = credentialTestTargetThrottleKey(user.ID, sshRequest)
		run = func() remoteexec.Result { return s.credentialTestSSHClient().Run(r.Context(), sshRequest) }
		label = "SSH"
	case targets.TransportWinRM:
		var winrmRequest winrmclient.Request
		winrmRequest, err = s.credentialTestWinRMRequest(request)
		targetThrottleKey = credentialTestWinRMTargetThrottleKey(user.ID, winrmRequest)
		run = func() remoteexec.Result { return s.credentialTestWinRMClient().Run(r.Context(), winrmRequest) }
		label = "WinRM"
	default:
		err = validationError("transport must be ssh or winrm")
	}
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		s.debugf("debug target credential test rejected user_id=%s transport=%s host=%s error=%q",
			user.ID, transport, host, tasks.RedactCredentialValues(err.Error(), request.Username, request.Password))
		status, payload := errorResponse(err)
		if status >= http.StatusInternalServerError {
			payload = ErrorResponse{Error: "private key file could not be prepared", Code: "credential_test_failed"}
			status = http.StatusBadRequest
		}
		if payload.Code == "" {
			payload.Code = "invalid_target"
		}
		writeJSON(w, status, payload)
		return
	}
	now := time.Now()
	throttle := s.targetCredentialTestThrottle()
	userThrottleKey := credentialTestUserThrottleKey(user.ID)
	if !throttle.allowUser(userThrottleKey, now) || !throttle.allowTarget(targetThrottleKey, now) {
		s.debugf("debug target credential test throttled user_id=%s transport=%s host=%s", user.ID, transport, host)
		writeCredentialTestThrottled(w)
		return
	}
	s.debugf("debug target credential test start user_id=%s transport=%s host=%s", user.ID, transport, host)
	result := run()
	s.debugf("debug target credential test finish user_id=%s transport=%s host=%s timed_out=%t exit_status=%s error_present=%t",
		user.ID, transport, host, result.TimedOut, formatCredentialTestExitStatus(result.ExitStatus), result.Error != "")
	throttle.recordUserAttempt(userThrottleKey, now)
	if result.TimedOut {
		throttle.recordTargetFailure(targetThrottleKey, now)
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: label + " connection timed out", Code: "credential_test_failed"})
		return
	}
	if result.ErrorCode == sshclient.ErrorCodeHostKeyUnknown || result.ErrorCode == sshclient.ErrorCodeHostKeyChanged {
		throttle.recordTargetFailure(targetThrottleKey, now)
		var hostKey *SSHHostKeyInfo
		if result.HostKey != nil {
			hostKey = &SSHHostKeyInfo{Host: result.HostKey.Host, Port: result.HostKey.Port, Algorithm: result.HostKey.Algorithm, Fingerprint: result.HostKey.Fingerprint}
		}
		writeJSON(w, http.StatusConflict, ErrorResponse{Error: result.Error, Code: result.ErrorCode, HostKey: hostKey})
		return
	}
	if result.Error != "" || (result.ExitStatus != nil && *result.ExitStatus != 0) {
		throttle.recordTargetFailure(targetThrottleKey, now)
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: label + " credential test failed", Code: "credential_test_failed"})
		return
	}
	throttle.recordTargetSuccess(targetThrottleKey)
	writeJSON(w, http.StatusOK, UserTargetCredentialTestResponse{Status: "success", Message: label + " connection verified"})
}

func formatCredentialTestExitStatus(status *int) string {
	if status == nil {
		return "nil"
	}
	return strconv.Itoa(*status)
}

func writeCredentialTestThrottled(w http.ResponseWriter) {
	writeJSON(w, http.StatusTooManyRequests, ErrorResponse{
		Error:     "too many credential test attempts",
		Code:      "credential_test_throttled",
		Retryable: true,
	})
}

func (s *Server) handleUserTarget(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/user/targets/"), "/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "target not found")
		return
	}
	if err := requireUUIDV4(id, "target_id"); err != nil {
		writeTargetMutationError(w, err)
		return
	}
	switch r.Method {
	case http.MethodPut:
		s.handleUpdateUserTarget(w, r, id)
	case http.MethodDelete:
		s.handleDeleteUserTarget(w, r, id)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleCreateUserTarget(w http.ResponseWriter, r *http.Request) {
	user, _, ok := userSessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
		return
	}
	paths, ok := s.managementPaths()
	if !ok {
		writeError(w, http.StatusInternalServerError, "management paths are not configured")
		return
	}
	request, ok := decodeTargetMutationRequest(w, r)
	if !ok {
		return
	}
	if err := validateTargetReferenceIDs(request); err != nil {
		writeTargetMutationError(w, err)
		return
	}
	if err := storeUploadedTargetPrivateKey(paths, &request); err != nil {
		writeTargetMutationError(w, err)
		return
	}
	target, err := targetFromMutationRequest(paths, request, user.ID, nil)
	if err != nil {
		writeTargetMutationError(w, err)
		return
	}
	result, err := users.CreateTarget(paths, target, nil)
	if err != nil {
		writeTargetMutationError(w, err)
		return
	}
	created, err := readStoredTarget(paths, result.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "created target could not be read")
		return
	}
	if err := s.refreshTargetRegistry(paths); err != nil {
		writeTargetMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, safeUserTargetResponse(created))
}

func (s *Server) handleUpdateUserTarget(w http.ResponseWriter, r *http.Request, id string) {
	user, _, ok := userSessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
		return
	}
	paths, ok := s.managementPaths()
	if !ok {
		writeError(w, http.StatusInternalServerError, "management paths are not configured")
		return
	}
	request, ok := decodeTargetMutationRequest(w, r)
	if !ok {
		return
	}
	if err := validateTargetReferenceIDs(request); err != nil {
		writeTargetMutationError(w, err)
		return
	}
	existing, err := readStoredTarget(paths, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "target not found")
		return
	}
	if !user.Admin && !containsUserID(existing.UserIDs, user.ID) {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "target is not owned by the current user", Code: "forbidden"})
		return
	}
	if err := storeUploadedTargetPrivateKey(paths, &request); err != nil {
		writeTargetMutationError(w, err)
		return
	}
	target, err := targetFromMutationRequest(paths, request, user.ID, &existing)
	if err != nil {
		writeTargetMutationError(w, err)
		return
	}
	if _, err := users.UpdateTarget(paths, id, target); err != nil {
		writeTargetMutationError(w, err)
		return
	}
	updated, err := readStoredTarget(paths, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "updated target could not be read")
		return
	}
	if err := s.refreshTargetRegistry(paths); err != nil {
		writeTargetMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, safeUserTargetResponse(updated))
}

func (s *Server) handleDeleteUserTarget(w http.ResponseWriter, r *http.Request, id string) {
	user, _, ok := userSessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing user session", Code: "auth_required"})
		return
	}
	paths, ok := s.managementPaths()
	if !ok {
		writeError(w, http.StatusInternalServerError, "management paths are not configured")
		return
	}
	existing, err := readStoredTarget(paths, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "target not found")
		return
	}
	if !user.Admin && !containsUserID(existing.UserIDs, user.ID) {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "target is not owned by the current user", Code: "forbidden"})
		return
	}
	if _, err := users.DeleteTarget(paths, id); err != nil {
		writeTargetMutationError(w, err)
		return
	}
	if err := s.refreshTargetRegistry(paths); err != nil {
		writeTargetMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func userTargetsResponse(items []targets.Target) UserTargetsResponse {
	sort.Slice(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})
	response := UserTargetsResponse{Targets: make([]UserTargetResponse, 0, len(items))}
	for _, target := range items {
		response.Targets = append(response.Targets, safeUserTargetResponse(target))
	}
	return response
}

func safeUserTargetResponse(target targets.Target) UserTargetResponse {
	authType := ""
	if target.Auth != nil {
		authType = target.Auth.Type
	}
	response := UserTargetResponse{
		ID:          target.ID,
		Transport:   target.Transport,
		OS:          target.OS,
		Hostname:    target.Hostname,
		IP:          target.IP,
		PolicyID:    target.PolicyID,
		AgentIDs:    append([]string(nil), target.AgentIDs...),
		UserIDs:     append([]string(nil), target.UserIDs...),
		Description: target.Description,
		AuthType:    authType,
	}
	if target.WinRM != nil {
		response.WinRM = &UserTargetWinRM{Scheme: target.WinRM.Scheme, Port: target.WinRM.Port, TLSInsecure: target.WinRM.TLSInsecure}
	}
	return response
}

func (s *Server) credentialTestSSHRequest(request UserTargetMutationRequest) (sshclient.Request, func(), error) {
	ip := strings.TrimSpace(request.IP)
	username := strings.TrimSpace(request.Username)
	authType := strings.TrimSpace(request.AuthType)
	if ip == "" {
		return sshclient.Request{}, nil, validationError("ip is required")
	}
	if username == "" {
		return sshclient.Request{}, nil, validationError("username is required")
	}
	if authType == "" {
		authType = targets.AuthTypePassword
	}
	sshRequest := sshclient.Request{
		Host:                       ip,
		Username:                   username,
		AuthType:                   authType,
		RemoteCommand:              "hostname",
		SensitiveValues:            []string{username},
		KnownHostsPath:             s.KnownHostsPath,
		ApprovedHostKeyFingerprint: strings.TrimSpace(request.ApprovedHostKeyFingerprint),
		Timeout:                    credentialTestTimeout(s.CommandTimeout),
		OutputLimit:                1024,
	}
	switch authType {
	case targets.AuthTypePassword:
		if strings.TrimSpace(request.Password) == "" {
			return sshclient.Request{}, nil, validationError("password is required")
		}
		sshRequest.AuthType = sshclient.AuthTypePassword
		sshRequest.Password = request.Password
		sshRequest.SensitiveValues = append(sshRequest.SensitiveValues, request.Password)
		return sshRequest, nil, nil
	case targets.AuthTypePrivateKey:
		if len(request.PrivateKeyUpload) == 0 {
			return sshclient.Request{}, nil, validationError("private key file is required")
		}
		privateKeyPath, cleanup, err := writeTemporaryPrivateKey(request.PrivateKeyUpload)
		if err != nil {
			return sshclient.Request{}, cleanup, err
		}
		sshRequest.AuthType = sshclient.AuthTypePrivateKey
		sshRequest.PrivateKeyPath = privateKeyPath
		return sshRequest, cleanup, nil
	default:
		return sshclient.Request{}, nil, validationError("auth_type must be password or private_key")
	}
}

func (s *Server) credentialTestSSHClient() sshclient.Client {
	if s.SSHClient != nil {
		return s.SSHClient
	}
	return sshclient.Runner{
		Timeout:               credentialTestTimeout(s.CommandTimeout),
		OutputLimit:           1024,
		DefaultKnownHostsPath: s.KnownHostsPath,
		Debug:                 s.Debug,
		Logger:                s.Logger,
	}
}

func (s *Server) credentialTestWinRMRequest(request UserTargetMutationRequest) (winrmclient.Request, error) {
	host := strings.TrimSpace(request.Hostname)
	if host == "" {
		host = strings.TrimSpace(request.IP)
	}
	if host == "" {
		return winrmclient.Request{}, validationError("hostname or ip is required")
	}
	username := strings.TrimSpace(request.Username)
	if username == "" {
		return winrmclient.Request{}, validationError("username is required")
	}
	if request.AuthType != "" && request.AuthType != targets.AuthTypePassword {
		return winrmclient.Request{}, validationError("WinRM targets require password authentication")
	}
	if strings.TrimSpace(request.Password) == "" {
		return winrmclient.Request{}, validationError("password is required")
	}
	if request.WinRM == nil {
		return winrmclient.Request{}, validationError("winrm is required")
	}
	scheme := strings.ToLower(strings.TrimSpace(request.WinRM.Scheme))
	if scheme != winrmclient.SchemeHTTP && scheme != winrmclient.SchemeHTTPS {
		return winrmclient.Request{}, validationError("winrm.scheme must be http or https")
	}
	port := request.WinRM.Port
	if port == 0 {
		if scheme == winrmclient.SchemeHTTPS {
			port = 5986
		} else {
			port = 5985
		}
	}
	if port < 1 || port > 65535 {
		return winrmclient.Request{}, validationError("winrm.port must be between 1 and 65535")
	}
	if request.WinRM.TLSInsecure && scheme != winrmclient.SchemeHTTPS {
		return winrmclient.Request{}, validationError("winrm.tls_insecure is valid only for https")
	}
	return winrmclient.Request{
		Host: host, Port: port, Scheme: scheme, TLSInsecure: request.WinRM.TLSInsecure,
		Username: username, Password: request.Password, PowerShell: "Write-Output PROXYMA_WINRM_OK",
		SensitiveValues: []string{username, request.Password}, Timeout: credentialTestWinRMTimeout(s.CommandTimeout), OutputLimit: 1024,
	}, nil
}

func (s *Server) credentialTestWinRMClient() winrmclient.Client {
	if s.WinRMClient != nil {
		return s.WinRMClient
	}
	return winrmclient.Runner{Timeout: credentialTestWinRMTimeout(s.CommandTimeout), OutputLimit: 1024, Debug: s.Debug, Logger: s.Logger}
}

func credentialTestTimeout(commandTimeout time.Duration) time.Duration {
	const defaultTimeout = 10 * time.Second
	if commandTimeout <= 0 || commandTimeout > defaultTimeout {
		return defaultTimeout
	}
	return commandTimeout
}

func credentialTestWinRMTimeout(commandTimeout time.Duration) time.Duration {
	const defaultTimeout = 30 * time.Second
	if commandTimeout <= 0 || commandTimeout > defaultTimeout {
		return defaultTimeout
	}
	return commandTimeout
}

func writeTemporaryPrivateKey(data []byte) (string, func(), error) {
	file, err := os.CreateTemp("", "Proxyma-private-key-test-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temporary private key: %w", err)
	}
	cleanup := func() {
		_ = os.Remove(file.Name())
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		cleanup()
		return "", nil, fmt.Errorf("secure temporary private key: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		cleanup()
		return "", nil, fmt.Errorf("write temporary private key: %w", err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close temporary private key: %w", err)
	}
	return file.Name(), cleanup, nil
}

func decodeTargetMutationRequest(w http.ResponseWriter, r *http.Request) (UserTargetMutationRequest, bool) {
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
		return decodeMultipartTargetMutationRequest(w, r)
	}
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxTargetBodyBytes))
	decoder.DisallowUnknownFields()
	var request UserTargetMutationRequest
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON target request", Code: "invalid_target"})
		return UserTargetMutationRequest{}, false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON target request", Code: "invalid_target"})
		return UserTargetMutationRequest{}, false
	}
	return request, true
}

func validateTargetReferenceIDs(request UserTargetMutationRequest) error {
	if err := requireOptionalUUIDV4(request.PolicyID, "policy_id"); err != nil {
		return err
	}
	if err := requireUUIDV4List(request.AgentIDs, "agent_ids"); err != nil {
		return err
	}
	return requireOptionalUUIDV4(request.TargetID, "target_id")
}

func decodeMultipartTargetMutationRequest(w http.ResponseWriter, r *http.Request) (UserTargetMutationRequest, bool) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxTargetBodyBytes)
	if err := r.ParseMultipartForm(maxTargetBodyBytes); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid multipart target request", Code: "invalid_target"})
		return UserTargetMutationRequest{}, false
	}
	form := r.MultipartForm
	request := UserTargetMutationRequest{
		Transport:                  firstFormValue(form.Value, "transport"),
		OS:                         firstFormValue(form.Value, "os"),
		Hostname:                   firstFormValue(form.Value, "hostname"),
		IP:                         firstFormValue(form.Value, "ip"),
		Description:                firstFormValue(form.Value, "description"),
		Username:                   firstFormValue(form.Value, "username"),
		PolicyID:                   firstFormValue(form.Value, "policy_id"),
		AgentIDs:                   formValues(form.Value, "agent_ids"),
		AuthType:                   firstFormValue(form.Value, "auth_type"),
		Password:                   firstFormValue(form.Value, "password"),
		TargetID:                   firstFormValue(form.Value, "target_id"),
		ApprovedHostKeyFingerprint: firstFormValue(form.Value, "approved_host_key_fingerprint"),
	}
	files := form.File["private_key_file"]
	if len(files) > 1 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "only one private key file may be uploaded", Code: "invalid_target"})
		return UserTargetMutationRequest{}, false
	}
	if len(files) == 1 {
		file, err := files[0].Open()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "private key file could not be read", Code: "invalid_target"})
			return UserTargetMutationRequest{}, false
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "private key file could not be read", Code: "invalid_target"})
			return UserTargetMutationRequest{}, false
		}
		if len(data) == 0 {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "private key file must not be empty", Code: "invalid_target"})
			return UserTargetMutationRequest{}, false
		}
		request.PrivateKeyUpload = data
	}
	return request, true
}

func firstFormValue(values map[string][]string, key string) string {
	items := values[key]
	if len(items) == 0 {
		return ""
	}
	return items[0]
}

func formValues(values map[string][]string, key string) []string {
	items, ok := values[key]
	if !ok {
		return nil
	}
	return append([]string(nil), items...)
}

func targetFromMutationRequest(paths users.Paths, request UserTargetMutationRequest, userID string, existing *targets.Target) (targets.Target, error) {
	target := targets.Target{
		Transport:   strings.TrimSpace(request.Transport),
		OS:          strings.TrimSpace(request.OS),
		Hostname:    strings.TrimSpace(request.Hostname),
		IP:          strings.TrimSpace(request.IP),
		Username:    strings.TrimSpace(request.Username),
		PolicyID:    strings.TrimSpace(request.PolicyID),
		AgentIDs:    cleanStringList(request.AgentIDs),
		Description: strings.TrimSpace(request.Description),
	}
	if existing == nil {
		target.UserIDs = []string{userID}
	} else {
		target.UserIDs = append([]string(nil), existing.UserIDs...)
		if target.Username == "" {
			target.Username = existing.Username
		}
		if target.Transport == "" {
			target.Transport = existing.Transport
		}
		if target.OS == "" {
			target.OS = existing.OS
		}
		if request.PolicyID == "" {
			target.PolicyID = existing.PolicyID
		}
		if request.AgentIDs == nil {
			target.AgentIDs = append([]string(nil), existing.AgentIDs...)
		}
	}
	if request.WinRM != nil {
		target.WinRM = &targets.WinRM{Scheme: request.WinRM.Scheme, Port: request.WinRM.Port, TLSInsecure: request.WinRM.TLSInsecure}
	} else if existing != nil && target.Transport == targets.TransportWinRM && existing.WinRM != nil {
		winrm := *existing.WinRM
		target.WinRM = &winrm
	}
	target.Auth = targetAuthFromMutationRequest(request, existing)
	if err := validateRuntimeTargetCredential(paths, target); err != nil {
		return targets.Target{}, err
	}
	return target, nil
}

func targetAuthFromMutationRequest(request UserTargetMutationRequest, existing *targets.Target) *targets.TargetAuth {
	authType := strings.TrimSpace(request.AuthType)
	if authType == "" && existing != nil {
		return cloneTargetAuth(existing.Auth)
	}
	switch authType {
	case targets.AuthTypePassword:
		if strings.TrimSpace(request.Password) != "" {
			return targets.PasswordAuth(request.Password)
		}
		if existing != nil && existing.Auth != nil && existing.Auth.Type == targets.AuthTypePassword {
			return cloneTargetAuth(existing.Auth)
		}
		return targets.PasswordAuth("")
	case targets.AuthTypePrivateKey:
		privateKeyPath := strings.TrimSpace(request.PrivateKeyPath)
		if privateKeyPath != "" {
			return targets.PrivateKeyAuth(privateKeyPath)
		}
		if existing != nil && existing.Auth != nil && existing.Auth.Type == targets.AuthTypePrivateKey {
			return cloneTargetAuth(existing.Auth)
		}
		return targets.PrivateKeyAuth("")
	default:
		return nil
	}
}

func cloneTargetAuth(auth *targets.TargetAuth) *targets.TargetAuth {
	if auth == nil {
		return nil
	}
	cloned := *auth
	return &cloned
}

func storeUploadedTargetPrivateKey(paths users.Paths, request *UserTargetMutationRequest) error {
	if request == nil || len(request.PrivateKeyUpload) == 0 {
		return nil
	}
	if strings.TrimSpace(request.AuthType) != targets.AuthTypePrivateKey {
		return validationError("private key file can only be uploaded for private key targets")
	}
	keyID, err := ids.NewUUIDV4()
	if err != nil {
		return fmt.Errorf("generate private key id: %w", err)
	}
	relativePath := filepath.ToSlash(filepath.Join(targets.PrivateKeyDirectory, keyID))
	privateKeyPath, err := targetPrivateKeyAbsolutePath(paths, relativePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(privateKeyPath), 0o700); err != nil {
		return fmt.Errorf("create private key directory: %w", err)
	}
	if err := os.WriteFile(privateKeyPath, request.PrivateKeyUpload, 0o600); err != nil {
		return fmt.Errorf("write private key file: %w", err)
	}
	request.PrivateKeyPath = relativePath
	request.PrivateKeyUpload = nil
	return nil
}

func validateRuntimeTargetCredential(paths users.Paths, target targets.Target) error {
	if target.Auth == nil || target.Auth.Type != targets.AuthTypePrivateKey || strings.TrimSpace(target.Auth.PrivateKeyPath) == "" {
		return nil
	}
	privateKeyPath, err := targetPrivateKeyAbsolutePath(paths, target.Auth.PrivateKeyPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(privateKeyPath); err != nil {
		return validationError("auth.private_key_path file not found")
	}
	return nil
}

func targetPrivateKeyAbsolutePath(paths users.Paths, relativePath string) (string, error) {
	relativePath = strings.TrimSpace(relativePath)
	if relativePath == "" {
		return "", validationError("auth.private_key_path is required")
	}
	cleanRelativePath := filepath.ToSlash(filepath.Clean(filepath.FromSlash(relativePath)))
	if strings.HasPrefix(cleanRelativePath, "../") || cleanRelativePath == ".." || filepath.IsAbs(relativePath) {
		return "", validationError("auth.private_key_path must be under secrets/private_keys")
	}
	privateKeyDirectory := filepath.ToSlash(filepath.Clean(filepath.FromSlash(targets.PrivateKeyDirectory)))
	if !strings.HasPrefix(cleanRelativePath, privateKeyDirectory+"/") {
		return "", validationError("auth.private_key_path must be under secrets/private_keys")
	}
	root := targets.ProjectRootForDir(paths.TargetDir)
	return filepath.Join(root, filepath.FromSlash(cleanRelativePath)), nil
}

func cleanStringList(values []string) []string {
	if values == nil {
		return nil
	}
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	return cleaned
}
