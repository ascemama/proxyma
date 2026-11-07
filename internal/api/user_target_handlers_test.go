package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"proxyma/internal/sshclient"
	"proxyma/internal/targets"
	"proxyma/internal/users"
	"strings"
	"testing"
	"time"
)

func TestUserTargetsEndpointAdminSeesAllTargetsAndRedactsCredentials(t *testing.T) {
	server := testLoginServer(t, false)
	owned := targets.Target{
		ID:          "8dc60324-b3bb-4d19-9aa6-0e149e4938b8",
		Hostname:    "owned.example",
		IP:          "10.0.0.1",
		Username:    "owned-user",
		PolicyID:    "baseline",
		AgentIDs:    []string{testAgent.ID},
		UserIDs:     []string{testUser.ID},
		Auth:        targets.PasswordAuth("owned-target-password"),
		Description: "owned target",
	}
	other := targets.Target{
		ID:       "7937dd2d-0b8a-4880-8afa-0ea60fd65a43",
		Hostname: "other.example",
		Username: "other-user",
		PolicyID: "6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c",
		AgentIDs: []string{testAgent.ID},
		UserIDs:  []string{"a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5"},
		Auth:     targets.PasswordAuth("other-target-password"),
	}
	shared := targets.Target{
		ID:       "a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5",
		Hostname: "shared.example",
		Username: "shared-user",
		PolicyID: "6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c",
		AgentIDs: []string{testAgent.ID},
		UserIDs:  []string{"7937dd2d-0b8a-4880-8afa-0ea60fd65a43", testUser.ID},
		Auth:     targets.PrivateKeyAuth("secrets/private_keys/shared-key"),
	}
	server.Targets = targets.NewRegistry([]targets.Target{owned, other, shared})
	cookie := loginCookie(t, server)

	req := httptest.NewRequest(http.MethodGet, "/v1/user/targets", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response UserTargetsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Targets) != 3 || response.Targets[0].ID != other.ID || response.Targets[1].ID != owned.ID || response.Targets[2].ID != shared.ID {
		t.Fatalf("response = %#v", response)
	}
	if response.Targets[0].AuthType != targets.AuthTypePassword || response.Targets[1].AuthType != targets.AuthTypePassword || response.Targets[2].AuthType != targets.AuthTypePrivateKey {
		t.Fatalf("auth types = %#v", response.Targets)
	}
	body := rec.Body.String()
	for _, leaked := range []string{"owned-target-password", "other-target-password", "secrets/private_keys/shared-key", "owned-user", "shared-user"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("target response leaked credential data %q: %s", leaked, body)
		}
	}
}

func TestUserTargetsEndpointNonAdminFiltersOwnedTargets(t *testing.T) {
	server := testLoginServer(t, false)
	currentUser, found := server.userRegistry().Find(testUser.ID)
	if !found {
		t.Fatal("missing current user")
	}
	currentUser.Admin = false
	server.setUserRegistry(users.NewRegistry([]users.User{currentUser}))
	owned := targets.Target{
		ID:          "8dc60324-b3bb-4d19-9aa6-0e149e4938b8",
		Hostname:    "owned.example",
		IP:          "10.0.0.1",
		Username:    "owned-user",
		PolicyID:    "baseline",
		AgentIDs:    []string{testAgent.ID},
		UserIDs:     []string{testUser.ID},
		Auth:        targets.PasswordAuth("owned-target-password"),
		Description: "owned target",
	}
	other := targets.Target{
		ID:       "7937dd2d-0b8a-4880-8afa-0ea60fd65a43",
		Hostname: "other.example",
		Username: "other-user",
		PolicyID: "6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c",
		AgentIDs: []string{testAgent.ID},
		UserIDs:  []string{"a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5"},
		Auth:     targets.PasswordAuth("other-target-password"),
	}
	shared := targets.Target{
		ID:       "a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5",
		Hostname: "shared.example",
		Username: "shared-user",
		PolicyID: "6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c",
		AgentIDs: []string{testAgent.ID},
		UserIDs:  []string{"7937dd2d-0b8a-4880-8afa-0ea60fd65a43", testUser.ID},
		Auth:     targets.PrivateKeyAuth("secrets/private_keys/shared-key"),
	}
	server.Targets = targets.NewRegistry([]targets.Target{owned, other, shared})
	cookie := loginCookie(t, server)

	req := httptest.NewRequest(http.MethodGet, "/v1/user/targets", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response UserTargetsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Targets) != 2 || response.Targets[0].ID != owned.ID || response.Targets[1].ID != shared.ID {
		t.Fatalf("response = %#v", response)
	}
}

func TestUserTargetMutationsCreateUpdateDeleteOwnedTarget(t *testing.T) {
	server, paths := testTargetMutationServer(t, nil)
	cookie, session := loginSession(t, server)

	createBody := `{
		"hostname":"NEW-SRV-01",
		"ip":"10.10.1.80",
		"description":"New managed target",
		"username":"fixture-new",
		"policy_id":"6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c",
		"agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],
		"auth_type":"password",
		"password":"new-target-secret"
	}`
	createRec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/targets", cookie, session.CSRFToken, createBody)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	if strings.Contains(createRec.Body.String(), "new-target-secret") || strings.Contains(createRec.Body.String(), "fixture-new") {
		t.Fatalf("create response leaked credential data: %s", createRec.Body.String())
	}
	var created UserTargetResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Hostname != "NEW-SRV-01" || created.AuthType != targets.AuthTypePassword {
		t.Fatalf("created response = %#v", created)
	}
	stored, err := readStoredTarget(paths, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Username != "fixture-new" || stored.Auth == nil || stored.Auth.Password != "new-target-secret" || len(stored.UserIDs) != 1 || stored.UserIDs[0] != testUser.ID {
		t.Fatalf("stored target = %#v", stored)
	}

	updateBody := `{
		"hostname":"NEW-SRV-02",
		"ip":"10.10.1.81",
		"description":"Updated managed target",
		"policy_id":"6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c",
		"agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],
		"auth_type":"password"
	}`
	updateRec := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/targets/"+created.ID, cookie, session.CSRFToken, updateBody)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", updateRec.Code, updateRec.Body.String())
	}
	updated, err := readStoredTarget(paths, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Hostname != "NEW-SRV-02" || updated.IP != "10.10.1.81" || updated.Description != "Updated managed target" {
		t.Fatalf("updated target metadata = %#v", updated)
	}
	if updated.Username != "fixture-new" || updated.Auth == nil || updated.Auth.Password != "new-target-secret" {
		t.Fatalf("update did not preserve credential fields = %#v", updated)
	}
	if _, found, duplicate := server.targetRegistry().MatchTarget("NEW-SRV-02"); !found || duplicate {
		t.Fatalf("refreshed registry did not contain updated target")
	}

	deleteRec := userTargetMutationRequest(t, server, http.MethodDelete, "/v1/user/targets/"+created.ID, cookie, session.CSRFToken, ``)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
	if _, err := readStoredTarget(paths, created.ID); !os.IsNotExist(err) {
		t.Fatalf("expected deleted target file, err=%v", err)
	}
	if _, found, duplicate := server.targetRegistry().MatchTarget("NEW-SRV-02"); found || duplicate {
		t.Fatalf("refreshed registry still matched deleted target")
	}
}

func TestUserTargetMutationsCreateWinRMTarget(t *testing.T) {
	server, paths := testTargetMutationServer(t, nil)
	cookie, session := loginSession(t, server)
	body := `{
		"transport":"winrm", "hostname":"win.example", "username":"Administrator",
		"policy_id":"6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c",
		"agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],
		"auth_type":"password", "password":"secret",
		"winrm":{"scheme":"https","tls_insecure":true}
	}`
	rec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/targets", cookie, session.CSRFToken, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response UserTargetResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	stored, err := readStoredTarget(paths, response.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Transport != targets.TransportWinRM || stored.WinRM == nil || stored.WinRM.Port != 5986 || !stored.WinRM.TLSInsecure || response.WinRM == nil || response.WinRM.Port != 5986 {
		t.Fatalf("stored=%#v response=%#v", stored, response)
	}
}

func TestUserTargetMutationsUploadPrivateKeyWithGUIDStorage(t *testing.T) {
	server, paths := testTargetMutationServer(t, nil)
	cookie, session := loginSession(t, server)

	createRec := userTargetMultipartMutationRequest(t, server, http.MethodPost, "/v1/user/targets", cookie, session.CSRFToken, map[string][]string{
		"hostname":    {"KEY-SRV-01"},
		"ip":          {"10.10.1.90"},
		"description": {"Private key target"},
		"username":    {"fixture-key"},
		"policy_id":   {"6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"},
		"agent_ids":   {testAgent.ID},
		"auth_type":   {targets.AuthTypePrivateKey},
	}, []byte("PRIVATE KEY ONE"))
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	if strings.Contains(createRec.Body.String(), "PRIVATE KEY ONE") || strings.Contains(createRec.Body.String(), "id_ed25519_Proxyma") {
		t.Fatalf("create response leaked private key data: %s", createRec.Body.String())
	}
	var created UserTargetResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	stored, err := readStoredTarget(paths, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	firstPath := stored.Auth.PrivateKeyPath
	assertGUIDPrivateKeyPath(t, firstPath)
	assertPrivateKeyFile(t, paths, firstPath, "PRIVATE KEY ONE")

	updateRec := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/targets/"+created.ID, cookie, session.CSRFToken, `{
		"hostname":"KEY-SRV-02",
		"ip":"10.10.1.91",
		"description":"Updated private key target",
		"auth_type":"private_key"
	}`)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", updateRec.Code, updateRec.Body.String())
	}
	stored, err = readStoredTarget(paths, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Auth.PrivateKeyPath != firstPath {
		t.Fatalf("private key path changed without replacement: got %q want %q", stored.Auth.PrivateKeyPath, firstPath)
	}

	replaceRec := userTargetMultipartMutationRequest(t, server, http.MethodPut, "/v1/user/targets/"+created.ID, cookie, session.CSRFToken, map[string][]string{
		"hostname":  {"KEY-SRV-03"},
		"auth_type": {targets.AuthTypePrivateKey},
	}, []byte("PRIVATE KEY TWO"))
	if replaceRec.Code != http.StatusOK {
		t.Fatalf("replace status = %d body=%s", replaceRec.Code, replaceRec.Body.String())
	}
	stored, err = readStoredTarget(paths, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Auth.PrivateKeyPath == firstPath {
		t.Fatalf("private key path did not change after replacement: %q", stored.Auth.PrivateKeyPath)
	}
	assertGUIDPrivateKeyPath(t, stored.Auth.PrivateKeyPath)
	assertPrivateKeyFile(t, paths, stored.Auth.PrivateKeyPath, "PRIVATE KEY TWO")

	emptyRec := userTargetMultipartMutationRequest(t, server, http.MethodPost, "/v1/user/targets", cookie, session.CSRFToken, map[string][]string{
		"hostname":  {"KEY-SRV-EMPTY"},
		"username":  {"fixture-key"},
		"policy_id": {"6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"},
		"agent_ids": {testAgent.ID},
		"auth_type": {targets.AuthTypePrivateKey},
	}, []byte{})
	if emptyRec.Code != http.StatusBadRequest {
		t.Fatalf("empty upload status = %d body=%s", emptyRec.Code, emptyRec.Body.String())
	}
}

func TestUserTargetCredentialTestPassword(t *testing.T) {
	server, _ := testTargetMutationServer(t, nil)
	fake := &fakeSSHClient{result: sshclient.Result{}}
	server.SSHClient = fake
	server.CommandTimeout = time.Minute
	server.KnownHostsPath = filepath.Join(t.TempDir(), "known_hosts")
	cookie, session := loginSession(t, server)

	rec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/targets/test-credentials", cookie, session.CSRFToken, `{
		"ip":"10.10.1.80",
		"username":"fixture-new",
		"auth_type":"password",
		"password":"new-target-secret"
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "new-target-secret") || strings.Contains(rec.Body.String(), "fixture-new") {
		t.Fatalf("credential test response leaked credential data: %s", rec.Body.String())
	}
	if len(fake.requests) != 1 {
		t.Fatalf("ssh requests = %d", len(fake.requests))
	}
	request := fake.requests[0]
	if request.Host != "10.10.1.80" || request.Username != "fixture-new" || request.AuthType != sshclient.AuthTypePassword || request.Password != "new-target-secret" {
		t.Fatalf("ssh request = %#v", request)
	}
	if request.RemoteCommand != "hostname" || request.Timeout != 10*time.Second || request.OutputLimit != 1024 || request.KnownHostsPath != server.KnownHostsPath {
		t.Fatalf("ssh request execution fields = %#v", request)
	}
	var response UserTargetCredentialTestResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Status != "success" || response.Message != "SSH connection verified" {
		t.Fatalf("response = %#v", response)
	}
	if targetsOnDisk := countJSONFiles(t, server.ManagementPaths.TargetDir); targetsOnDisk != 0 {
		t.Fatalf("credential test created target files: %d", targetsOnDisk)
	}
}

func TestUserTargetCredentialTestWinRM(t *testing.T) {
	server, _ := testTargetMutationServer(t, nil)
	fake := &fakeWinRMClient{}
	server.WinRMClient = fake
	server.CommandTimeout = time.Minute
	cookie, session := loginSession(t, server)
	rec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/targets/test-credentials", cookie, session.CSRFToken, `{
		"transport":"winrm",
		"hostname":"win.example",
		"username":"Administrator",
		"auth_type":"password",
		"password":"secret",
		"winrm":{"scheme":"https","port":5986,"tls_insecure":true}
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(fake.requests) != 1 {
		t.Fatalf("requests=%#v", fake.requests)
	}
	request := fake.requests[0]
	if request.Host != "win.example" || request.Scheme != "https" || request.Port != 5986 || !request.TLSInsecure || request.PowerShell != "Write-Output PROXYMA_WINRM_OK" || request.Timeout != 30*time.Second {
		t.Fatalf("request=%#v", request)
	}
}

func TestUserTargetCredentialTestFailureIsSecretSafe(t *testing.T) {
	server, _ := testTargetMutationServer(t, nil)
	server.SSHClient = &fakeSSHClient{result: sshclient.Result{Error: "permission denied for new-target-secret"}}
	cookie, session := loginSession(t, server)

	rec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/targets/test-credentials", cookie, session.CSRFToken, `{
		"ip":"10.10.1.80",
		"username":"fixture-new",
		"auth_type":"password",
		"password":"new-target-secret"
	}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, forbidden := range []string{"new-target-secret", "permission denied"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("credential test response leaked %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, "SSH credential test failed") {
		t.Fatalf("response body = %s", body)
	}
}

func TestUserTargetCredentialTestPrivateKeyUsesTemporaryFile(t *testing.T) {
	server, paths := testTargetMutationServer(t, nil)
	fake := &fakeSSHClient{result: sshclient.Result{}}
	server.SSHClient = fake
	cookie, session := loginSession(t, server)

	rec := userTargetMultipartMutationRequest(t, server, http.MethodPost, "/v1/user/targets/test-credentials", cookie, session.CSRFToken, map[string][]string{
		"ip":        {"10.10.1.90"},
		"username":  {"fixture-key"},
		"auth_type": {targets.AuthTypePrivateKey},
	}, []byte("PRIVATE KEY ONE"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "PRIVATE KEY ONE") || strings.Contains(rec.Body.String(), "id_ed25519_Proxyma") {
		t.Fatalf("credential test response leaked private key data: %s", rec.Body.String())
	}
	if len(fake.requests) != 1 {
		t.Fatalf("ssh requests = %d", len(fake.requests))
	}
	request := fake.requests[0]
	if request.AuthType != sshclient.AuthTypePrivateKey || request.PrivateKeyPath == "" {
		t.Fatalf("ssh request = %#v", request)
	}
	if _, err := os.Stat(request.PrivateKeyPath); !os.IsNotExist(err) {
		t.Fatalf("temporary private key was not deleted, stat err=%v path=%q", err, request.PrivateKeyPath)
	}
	storedPrivateKeyDir := filepath.Join(targets.ProjectRootForDir(paths.TargetDir), filepath.FromSlash(targets.PrivateKeyDirectory))
	if _, err := os.Stat(storedPrivateKeyDir); !os.IsNotExist(err) {
		t.Fatalf("credential test created stored private key directory, err=%v", err)
	}
	if targetsOnDisk := countJSONFiles(t, paths.TargetDir); targetsOnDisk != 0 {
		t.Fatalf("credential test created target files: %d", targetsOnDisk)
	}
}

func TestUserTargetCredentialTestPrivateKeyFailureIsSecretSafe(t *testing.T) {
	server, _ := testTargetMutationServer(t, nil)
	server.SSHClient = &fakeSSHClient{result: sshclient.Result{Error: "private key rejected"}}
	cookie, session := loginSession(t, server)

	rec := userTargetMultipartMutationRequest(t, server, http.MethodPost, "/v1/user/targets/test-credentials", cookie, session.CSRFToken, map[string][]string{
		"ip":        {"10.10.1.90"},
		"username":  {"fixture-key"},
		"auth_type": {targets.AuthTypePrivateKey},
	}, []byte("PRIVATE KEY ONE"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, forbidden := range []string{"PRIVATE KEY ONE", "id_ed25519_Proxyma", "private key rejected"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("credential test response leaked %q: %s", forbidden, body)
		}
	}
}

func TestUserTargetCredentialTestThrottleBlocksSixthFailureForSameCredential(t *testing.T) {
	server, _ := testTargetMutationServer(t, nil)
	fake := &fakeSSHClient{result: sshclient.Result{Error: "permission denied for new-target-secret"}}
	server.SSHClient = fake
	cookie, session := loginSession(t, server)
	body := `{
		"ip":"10.10.1.80",
		"username":"fixture-new",
		"auth_type":"password",
		"password":"new-target-secret"
	}`

	for i := 0; i < maxCredentialTestFailures; i++ {
		rec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/targets/test-credentials", cookie, session.CSRFToken, body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("failure %d status = %d body=%s", i+1, rec.Code, rec.Body.String())
		}
	}
	if len(fake.requests) != maxCredentialTestFailures {
		t.Fatalf("ssh requests = %d", len(fake.requests))
	}

	throttled := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/targets/test-credentials", cookie, session.CSRFToken, body)
	assertCredentialTestThrottled(t, throttled, "new-target-secret")
	if len(fake.requests) != maxCredentialTestFailures {
		t.Fatalf("throttled request reached ssh: %d", len(fake.requests))
	}
}

func TestUserTargetCredentialTestThrottleClearsTargetFailuresAfterSuccess(t *testing.T) {
	server, _ := testTargetMutationServer(t, nil)
	fake := &fakeSSHClient{result: sshclient.Result{Error: "permission denied"}}
	server.SSHClient = fake
	cookie, session := loginSession(t, server)
	body := `{
		"ip":"10.10.1.80",
		"username":"fixture-new",
		"auth_type":"password",
		"password":"new-target-secret"
	}`

	for i := 0; i < maxCredentialTestFailures-1; i++ {
		rec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/targets/test-credentials", cookie, session.CSRFToken, body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("initial failure %d status = %d body=%s", i+1, rec.Code, rec.Body.String())
		}
	}

	fake.result = sshclient.Result{}
	success := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/targets/test-credentials", cookie, session.CSRFToken, body)
	if success.Code != http.StatusOK {
		t.Fatalf("success status = %d body=%s", success.Code, success.Body.String())
	}

	fake.result = sshclient.Result{Error: "permission denied"}
	for i := 0; i < maxCredentialTestFailures; i++ {
		rec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/targets/test-credentials", cookie, session.CSRFToken, body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("post-success failure %d status = %d body=%s", i+1, rec.Code, rec.Body.String())
		}
	}
	throttled := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/targets/test-credentials", cookie, session.CSRFToken, body)
	assertCredentialTestThrottled(t, throttled, "new-target-secret")
}

func TestUserTargetCredentialTestThrottleBlocksThirtyFirstAttemptForUser(t *testing.T) {
	server, _ := testTargetMutationServer(t, nil)
	fake := &fakeSSHClient{result: sshclient.Result{}}
	server.SSHClient = fake
	cookie, session := loginSession(t, server)

	for i := 0; i < maxCredentialTestAttempts; i++ {
		rec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/targets/test-credentials", cookie, session.CSRFToken, credentialTestBodyForIP(i))
		if rec.Code != http.StatusOK {
			t.Fatalf("attempt %d status = %d body=%s", i+1, rec.Code, rec.Body.String())
		}
	}
	if len(fake.requests) != maxCredentialTestAttempts {
		t.Fatalf("ssh requests = %d", len(fake.requests))
	}
	throttled := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/targets/test-credentials", cookie, session.CSRFToken, credentialTestBodyForIP(maxCredentialTestAttempts))
	assertCredentialTestThrottled(t, throttled, "new-target-secret")
	if len(fake.requests) != maxCredentialTestAttempts {
		t.Fatalf("throttled request reached ssh: %d", len(fake.requests))
	}
}

func TestUserTargetCredentialTestThrottleIsBoundToLoggedInUser(t *testing.T) {
	server, _ := testTargetMutationServer(t, nil)
	fake := &fakeSSHClient{result: sshclient.Result{}}
	server.SSHClient = fake
	cookie, session := loginSession(t, server)

	for i := 0; i < maxCredentialTestAttempts; i++ {
		rec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/targets/test-credentials", cookie, session.CSRFToken, credentialTestBodyForIP(i))
		if rec.Code != http.StatusOK {
			t.Fatalf("attempt %d status = %d body=%s", i+1, rec.Code, rec.Body.String())
		}
	}
	throttled := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/targets/test-credentials", cookie, session.CSRFToken, credentialTestBodyForIP(maxCredentialTestAttempts))
	assertCredentialTestThrottled(t, throttled, "new-target-secret")

	otherCookie, otherCSRF := addAuthenticatedTestUser(t, server, users.User{
		ID:       "5c4a6222-d1ef-4ba6-a713-d17ceda67d66",
		Username: "bernard",
		Name:     "Bernard",
	})
	otherRec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/targets/test-credentials", otherCookie, otherCSRF, credentialTestBodyForIP(maxCredentialTestAttempts))
	if otherRec.Code != http.StatusOK {
		t.Fatalf("other user status = %d body=%s", otherRec.Code, otherRec.Body.String())
	}
}

func TestUserTargetCredentialTestRejectsMissingFieldsAndCSRF(t *testing.T) {
	server, _ := testTargetMutationServer(t, nil)
	fake := &fakeSSHClient{result: sshclient.Result{}}
	server.SSHClient = fake
	cookie, session := loginSession(t, server)

	cases := []struct {
		name string
		body string
	}{
		{name: "missing ip", body: `{"username":"fixture-new","auth_type":"password","password":"secret"}`},
		{name: "missing username", body: `{"ip":"10.10.1.80","auth_type":"password","password":"secret"}`},
		{name: "missing password", body: `{"ip":"10.10.1.80","username":"fixture-new","auth_type":"password"}`},
		{name: "missing private key", body: `{"ip":"10.10.1.80","username":"fixture-new","auth_type":"private_key"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/targets/test-credentials", cookie, session.CSRFToken, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}

	missingCSRF := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/targets/test-credentials", cookie, "", `{
		"ip":"10.10.1.80",
		"username":"fixture-new",
		"auth_type":"password",
		"password":"secret"
	}`)
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d body=%s", missingCSRF.Code, missingCSRF.Body.String())
	}
	if len(fake.requests) != 0 {
		t.Fatalf("invalid requests reached ssh: %d", len(fake.requests))
	}

	valid := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/targets/test-credentials", cookie, session.CSRFToken, `{
		"ip":"10.10.1.80",
		"username":"fixture-new",
		"auth_type":"password",
		"password":"secret"
	}`)
	if valid.Code != http.StatusOK {
		t.Fatalf("valid status = %d body=%s", valid.Code, valid.Body.String())
	}
	if len(fake.requests) != 1 {
		t.Fatalf("valid request did not reach ssh once: %d", len(fake.requests))
	}
}

func TestUserTargetMutationsAllowDuplicateHostnameForDifferentCredentials(t *testing.T) {
	server, paths := testTargetMutationServer(t, []targets.Target{
		{
			ID:       "8dc60324-b3bb-4d19-9aa6-0e149e4938b8",
			Hostname: "shared.example",
			IP:       "10.10.1.95",
			Username: "first-user",
			PolicyID: "6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c",
			AgentIDs: []string{testAgent.ID},
			UserIDs:  []string{testUser.ID},
			Auth:     targets.PasswordAuth("first-secret"),
		},
	})
	cookie, session := loginSession(t, server)
	createBody := `{
		"hostname":"shared.example",
		"ip":"10.10.1.95",
		"description":"Same VM with a different credential",
		"username":"second-user",
		"policy_id":"6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c",
		"agent_ids":["df9ec8bb-1a22-4181-a7c8-126ad553221d"],
		"auth_type":"password",
		"password":"second-secret"
	}`
	createRec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/targets", cookie, session.CSRFToken, createBody)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create duplicate status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created UserTargetResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	stored, err := readStoredTarget(paths, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Hostname != "shared.example" || stored.IP != "10.10.1.95" || stored.Username != "second-user" {
		t.Fatalf("stored target = %#v", stored)
	}
	if all := server.targetRegistry().All(); len(all) != 2 {
		t.Fatalf("target registry length = %d, want 2", len(all))
	}
}

func TestUserTargetMutationsRejectUnauthenticatedInvalidAndForeignRequests(t *testing.T) {
	foreignID := "7937dd2d-0b8a-4880-8afa-0ea60fd65a43"
	server, _ := testTargetMutationServer(t, []targets.Target{
		{
			ID:       foreignID,
			Hostname: "foreign.example",
			Username: "foreign-user",
			PolicyID: "6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c",
			AgentIDs: []string{testAgent.ID},
			UserIDs:  []string{"a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5"},
			Auth:     targets.PasswordAuth("foreign-secret"),
		},
	})
	currentUser, found := server.userRegistry().Find(testUser.ID)
	if !found {
		t.Fatal("missing current user")
	}
	currentUser.Admin = false
	server.setUserRegistry(users.NewRegistry([]users.User{currentUser}))

	unauthenticated := httptest.NewRecorder()
	server.Routes().ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodPost, "/v1/user/targets", strings.NewReader(`{}`)))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d body=%s", unauthenticated.Code, unauthenticated.Body.String())
	}

	cookie, session := loginSession(t, server)
	invalid := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/targets", cookie, session.CSRFToken, `{"hostname":"BROKEN"}`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid status = %d body=%s", invalid.Code, invalid.Body.String())
	}

	missingCSRF := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/targets", cookie, "", `{"hostname":"NO-CSRF"}`)
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatalf("missing csrf status = %d body=%s", missingCSRF.Code, missingCSRF.Body.String())
	}

	updateForeign := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/targets/"+foreignID, cookie, session.CSRFToken, `{"hostname":"foreign.example"}`)
	if updateForeign.Code != http.StatusForbidden {
		t.Fatalf("foreign update status = %d body=%s", updateForeign.Code, updateForeign.Body.String())
	}
	deleteForeign := userTargetMutationRequest(t, server, http.MethodDelete, "/v1/user/targets/"+foreignID, cookie, session.CSRFToken, ``)
	if deleteForeign.Code != http.StatusForbidden {
		t.Fatalf("foreign delete status = %d body=%s", deleteForeign.Code, deleteForeign.Body.String())
	}
}

func TestUserTargetMutationsAdminCanUpdateAndDeleteForeignTarget(t *testing.T) {
	foreignID := "7937dd2d-0b8a-4880-8afa-0ea60fd65a43"
	server, paths := testTargetMutationServer(t, []targets.Target{
		{
			ID:       foreignID,
			Hostname: "foreign.example",
			Username: "foreign-user",
			PolicyID: "6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c",
			AgentIDs: []string{testAgent.ID},
			UserIDs:  []string{"a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5"},
			Auth:     targets.PasswordAuth("foreign-secret"),
		},
	})
	adminUser, found := server.userRegistry().Find(testUser.ID)
	if !found {
		t.Fatal("missing admin user")
	}
	foreignUser := users.User{
		ID:         "a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5",
		Username:   "operator",
		Name:       "Operator",
		Admin:      false,
		Status:     users.StatusActive,
		PasswdHash: adminUser.PasswdHash,
	}
	writeJSONRecord(t, paths.UserDir, foreignUser.ID, foreignUser)
	server.setUserRegistry(users.NewRegistry([]users.User{adminUser, foreignUser}))
	cookie, session := loginSession(t, server)

	updateForeign := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/targets/"+foreignID, cookie, session.CSRFToken, `{"hostname":"foreign-updated.example"}`)
	if updateForeign.Code != http.StatusOK {
		t.Fatalf("foreign update status = %d body=%s", updateForeign.Code, updateForeign.Body.String())
	}
	updated, err := readStoredTarget(paths, foreignID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Hostname != "foreign-updated.example" || len(updated.UserIDs) != 1 || updated.UserIDs[0] != "a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5" {
		t.Fatalf("updated target = %#v", updated)
	}

	deleteForeign := userTargetMutationRequest(t, server, http.MethodDelete, "/v1/user/targets/"+foreignID, cookie, session.CSRFToken, ``)
	if deleteForeign.Code != http.StatusOK {
		t.Fatalf("foreign delete status = %d body=%s", deleteForeign.Code, deleteForeign.Body.String())
	}
	if _, err := readStoredTarget(paths, foreignID); !os.IsNotExist(err) {
		t.Fatalf("expected deleted target file, err=%v", err)
	}
}
