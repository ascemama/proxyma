package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"proxyma/internal/agents"
	"proxyma/internal/ids"
	"proxyma/internal/interaction/history"
	"proxyma/internal/targets"
	"proxyma/internal/taskflow/policy"
	"proxyma/internal/users"
	"strings"
	"testing"
	"time"
)

func containsHistoryTask(items []history.HistoryItem, taskID string) bool {
	for _, item := range items {
		if item.TaskID == taskID && item.EntryType == history.EntryTypeCommand {
			return true
		}
	}
	return false
}

func testLoginServer(t *testing.T, allowInsecureCookies bool) *Server {
	t.Helper()
	server := testServer(t)
	hash, err := users.GeneratePasswordHash([]byte("correct-password"))
	if err != nil {
		t.Fatal(err)
	}
	user := testUser
	user.PasswdHash = hash
	server.Users = users.NewRegistry([]users.User{user})
	server.UserSessions = users.NewSessionStore(t.TempDir())
	server.CredentialTokens = users.NewCredentialTokenStore(t.TempDir())
	server.UserActivationTokenTTL = 12 * time.Hour
	server.UserActivationTokenMaxTTL = 48 * time.Hour
	server.UserSessionTTL = time.Hour
	server.UseDevelopmentCookies = allowInsecureCookies
	server.throttle = newLoginThrottle()
	return server
}

func indexTasksForHistory(t *testing.T, server *Server) {
	t.Helper()
}

func loginCookie(t *testing.T, server *Server) *http.Cookie {
	t.Helper()
	cookie, _ := loginSession(t, server)
	return cookie
}

func loginSession(t *testing.T, server *Server) (*http.Cookie, UserSessionResponse) {
	t.Helper()
	loginRec := loginRequest(t, server, `{"username":"demo-admin","password":"correct-password"}`)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", loginRec.Code, loginRec.Body.String())
	}
	var response UserSessionResponse
	if err := json.Unmarshal(loginRec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return requireCookie(t, loginRec, userSessionCookieName), response
}

func loginRequest(t *testing.T, server *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/user/login", strings.NewReader(body))
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	return rec
}

func userTargetMutationRequest(t *testing.T, server *Server, method string, path string, cookie *http.Cookie, csrfToken string, body string) *httptest.ResponseRecorder {
	t.Helper()
	if strings.HasPrefix(path, "/v1/user/targets") && strings.TrimSpace(body) != "" {
		var payload map[string]any
		if json.Unmarshal([]byte(body), &payload) == nil {
			if _, ok := payload["transport"]; !ok {
				payload["transport"] = targets.TransportSSH
			}
			if _, ok := payload["os"]; !ok {
				if payload["transport"] == targets.TransportWinRM {
					payload["os"] = targets.OSWindows
				} else {
					payload["os"] = targets.OSLinux
				}
			}
			if encoded, err := json.Marshal(payload); err == nil {
				body = string(encoded)
			}
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if csrfToken != "" {
		req.Header.Set(userSessionCSRFHeader, csrfToken)
	}
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	return rec
}

func userTargetMultipartMutationRequest(t *testing.T, server *Server, method string, path string, cookie *http.Cookie, csrfToken string, fields map[string][]string, privateKey []byte) *httptest.ResponseRecorder {
	t.Helper()
	if _, ok := fields["transport"]; !ok {
		fields["transport"] = []string{targets.TransportSSH}
	}
	if _, ok := fields["os"]; !ok {
		if values := fields["transport"]; len(values) > 0 && values[0] == targets.TransportWinRM {
			fields["os"] = []string{targets.OSWindows}
		} else {
			fields["os"] = []string{targets.OSLinux}
		}
	}
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for key, values := range fields {
		for _, value := range values {
			if err := writer.WriteField(key, value); err != nil {
				t.Fatal(err)
			}
		}
	}
	part, err := writer.CreateFormFile("private_key_file", "id_ed25519_Proxyma")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(privateKey); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if csrfToken != "" {
		req.Header.Set(userSessionCSRFHeader, csrfToken)
	}
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	return rec
}

func assertGUIDPrivateKeyPath(t *testing.T, path string) {
	t.Helper()
	prefix := targets.PrivateKeyDirectory + "/"
	if !strings.HasPrefix(path, prefix) {
		t.Fatalf("private key path = %q, want prefix %q", path, prefix)
	}
	keyID := strings.TrimPrefix(path, prefix)
	if !ids.IsUUIDV4(keyID) {
		t.Fatalf("private key id = %q, want UUID v4", keyID)
	}
}

func assertPrivateKeyFile(t *testing.T, paths users.Paths, relativePath string, want string) {
	t.Helper()
	root := targets.ProjectRootForDir(paths.TargetDir)
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relativePath)))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("private key contents = %q, want %q", string(data), want)
	}
}

func countJSONFiles(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			count++
		}
	}
	return count
}

func assertCredentialTestThrottled(t *testing.T, rec *httptest.ResponseRecorder, forbidden ...string) {
	t.Helper()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "too many credential test attempts" || response.Code != "credential_test_throttled" || !response.Retryable {
		t.Fatalf("response = %#v", response)
	}
	for _, value := range forbidden {
		if strings.TrimSpace(value) != "" && strings.Contains(rec.Body.String(), value) {
			t.Fatalf("throttle response leaked %q: %s", value, rec.Body.String())
		}
	}
}

func credentialTestBodyForIP(index int) string {
	return fmt.Sprintf(`{
		"transport":"ssh",
		"ip":"10.10.2.%d",
		"username":"fixture-new",
		"auth_type":"password",
		"password":"new-target-secret"
	}`, index+1)
}

func addAuthenticatedTestUser(t *testing.T, server *Server, user users.User) (*http.Cookie, string) {
	t.Helper()
	user.Status = users.StatusActive
	items := server.Users.All()
	items = append(items, user)
	server.Users = users.NewRegistry(items)
	session, cookieValue, csrfToken, err := server.UserSessions.Create(user.ID, time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: server.userSessionCookieName(), Value: cookieValue, Path: "/", Expires: session.ExpiresAt}, csrfToken
}

func testTargetMutationServer(t *testing.T, initialTargets []targets.Target) (*Server, users.Paths) {
	t.Helper()
	server := testLoginServer(t, false)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module proxymatest\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := users.Paths{
		UserDir:              filepath.Join(root, "users"),
		CredentialTokenDir:   filepath.Join(root, "credential_tokens"),
		AgentDir:             filepath.Join(root, "agents"),
		TargetDir:            filepath.Join(root, "targets"),
		PolicyDir:            filepath.Join(root, "policies"),
		InteractionConfigDir: filepath.Join(root, "interactions"),
	}
	for _, dir := range []string{paths.UserDir, paths.CredentialTokenDir, paths.AgentDir, paths.TargetDir, paths.PolicyDir, paths.InteractionConfigDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	user, found := server.Users.Find(testUser.ID)
	if !found {
		t.Fatal("missing test user")
	}
	writeJSONRecord(t, paths.UserDir, user.ID, user)

	agent := testAgent
	agent.UserIDs = []string{testUser.ID}
	server.Agents = agents.NewRegistry([]agents.Agent{agent})
	writeJSONRecord(t, paths.AgentDir, agent.ID, agent)

	baselinePolicy := map[string]any{
		"id":            "6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c",
		"owner_user_id": testUser.ID,
		"name":          "Test baseline policy",
		"description":   "Test baseline policy",
		"enabled":       true,
		"rules": []map[string]any{
			{
				"id":               "allow-all",
				"effect":           "allow",
				"command_template": "hostname",
				"reason":           "test policy",
			},
		},
	}
	writeJSONRecord(t, paths.PolicyDir, "baseline", baselinePolicy)

	for _, target := range initialTargets {
		if target.Transport == "" {
			target.Transport = targets.TransportSSH
		}
		if target.OS == "" {
			if target.Transport == targets.TransportWinRM {
				target.OS = targets.OSWindows
			} else {
				target.OS = targets.OSLinux
			}
		}
		writeJSONRecord(t, paths.TargetDir, target.ID, target)
	}
	registry, diagnostics, err := targets.LoadDir(paths.TargetDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) > 0 {
		t.Fatalf("target diagnostics = %#v", diagnostics)
	}
	server.Targets = registry
	server.ManagementPaths = paths
	server.CredentialTokens = users.NewCredentialTokenStore(paths.CredentialTokenDir)
	policies, policyDiagnostics, err := policy.LoadDir(paths.PolicyDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(policyDiagnostics) > 0 {
		t.Fatalf("policy diagnostics = %#v", policyDiagnostics)
	}
	server.Policies = policy.NewMutableEvaluator(policies)
	return server, paths
}

func testInteractionMutationServer(t *testing.T) (*Server, users.Paths) {
	t.Helper()
	server, paths := testTargetMutationServer(t, nil)
	if err := os.MkdirAll(filepath.Join(paths.InteractionConfigDir, "ntfy"), 0o755); err != nil {
		t.Fatal(err)
	}
	return server, paths
}

func writeTestNtfyInteraction(t *testing.T, dir string, name string, id string, creatorUserID string, displayName string, token string) {
	t.Helper()
	value := map[string]any{
		"$schema":         "../../schemas/proxyma.interaction.ntfy.schema.json",
		"id":              id,
		"creator_user_id": creatorUserID,
		"display_name":    displayName,
		"enabled":         true,
		"base_url":        "https://ntfy.example",
		"request_topic":   id + "-requests",
		"response_topic":  id + "-responses",
		"token":           token,
		"poll_interval":   "2s",
		"poll_timeout":    "3s",
		"responded_by":    "ntfy",
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	ntfyDir := filepath.Join(dir, "ntfy")
	if err := os.MkdirAll(ntfyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ntfyDir, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeJSONRecord(t *testing.T, dir string, id string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func requireCookie(t *testing.T, rec *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("missing cookie %q in %s", name, rec.Header().Get("Set-Cookie"))
	return nil
}
