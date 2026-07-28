package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"proxyma/internal/remoteexec"
	"proxyma/internal/sshclient"
	"proxyma/internal/targets"
)

func TestCredentialProofIsOneUseAndBound(t *testing.T) {
	store := newCredentialProofStore()
	request := UserTargetMutationRequest{Transport: "ssh", IP: "10.0.0.1", Username: "demo", AuthType: "password", Password: "secret"}
	digest := credentialRequestDigest(request)
	token, err := store.issue("user-a", "target-a", digest)
	if err != nil {
		t.Fatal(err)
	}
	if store.consume(token, "user-b", "target-a", digest) {
		t.Fatal("proof accepted for another user")
	}
	if store.consume(token, "user-a", "target-b", digest) {
		t.Fatal("proof accepted for another target")
	}
	changed := request
	changed.Password = "different"
	if store.consume(token, "user-a", "target-a", credentialRequestDigest(changed)) {
		t.Fatal("proof accepted for changed credentials")
	}
	if !store.consume(token, "user-a", "target-a", digest) {
		t.Fatal("valid proof rejected")
	}
	if store.consume(token, "user-a", "target-a", digest) {
		t.Fatal("proof reused")
	}
}

func TestCredentialTestReturnsHostKeyChallenge(t *testing.T) {
	server, _ := testTargetMutationServer(t, nil)
	server.SSHClient = &fakeSSHClient{result: sshclient.Result{
		Error: "SSH host key is not trusted", ErrorCode: sshclient.ErrorCodeHostKeyUnknown,
		HostKey: &remoteexec.HostKeyInfo{Host: "10.0.0.1", Port: 22, Algorithm: "ssh-ed25519", Fingerprint: "SHA256:test"},
	}}
	cookie, session := loginSession(t, server)
	rec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/targets/test-credentials", cookie, session.CSRFToken, `{
		"transport":"ssh","ip":"10.0.0.1","username":"demo","auth_type":"password","password":"secret"
	}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code != sshclient.ErrorCodeHostKeyUnknown || response.HostKey == nil || response.HostKey.Fingerprint != "SHA256:test" {
		t.Fatalf("response = %#v", response)
	}
}

func TestTargetCreateRequiresAndConsumesCredentialProof(t *testing.T) {
	server, _ := testTargetMutationServer(t, nil)
	server.SSHClient = &fakeSSHClient{result: sshclient.Result{}}
	cookie, session := loginSession(t, server)
	request := UserTargetMutationRequest{
		Transport: targets.TransportSSH, OS: targets.OSLinux, Hostname: "new.example", IP: "10.0.0.8", Username: "demo",
		PolicyIDs: []string{"6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"}, AgentIDs: []string{testAgent.ID},
		AuthType: targets.AuthTypePassword, Password: "secret",
	}

	missing := rawTargetRequest(t, server, http.MethodPost, "/v1/user/targets", cookie, session.CSRFToken, request)
	if missing.Code != http.StatusBadRequest || !bytes.Contains(missing.Body.Bytes(), []byte("credential_test_required")) {
		t.Fatalf("missing proof status=%d body=%s", missing.Code, missing.Body.String())
	}

	testRec := rawTargetRequest(t, server, http.MethodPost, "/v1/user/targets/test-credentials", cookie, session.CSRFToken, request)
	if testRec.Code != http.StatusOK {
		t.Fatalf("test status=%d body=%s", testRec.Code, testRec.Body.String())
	}
	var tested UserTargetCredentialTestResponse
	if err := json.Unmarshal(testRec.Body.Bytes(), &tested); err != nil {
		t.Fatal(err)
	}
	if tested.CredentialTestToken == "" {
		t.Fatal("missing credential test token")
	}
	request.CredentialTestToken = tested.CredentialTestToken
	created := rawTargetRequest(t, server, http.MethodPost, "/v1/user/targets", cookie, session.CSRFToken, request)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	reused := rawTargetRequest(t, server, http.MethodPost, "/v1/user/targets", cookie, session.CSRFToken, request)
	if reused.Code != http.StatusBadRequest || !bytes.Contains(reused.Body.Bytes(), []byte("credential_test_required")) {
		t.Fatalf("reused proof status=%d body=%s", reused.Code, reused.Body.String())
	}
}

func rawTargetRequest(t *testing.T, server *Server, method, path string, cookie *http.Cookie, csrf string, payload UserTargetMutationRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(userSessionCSRFHeader, csrf)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	return rec
}

func TestCredentialProofExpires(t *testing.T) {
	store := newCredentialProofStore()
	now := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	token, err := store.issue("user", "", "digest")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(credentialProofTTL + time.Second)
	if store.consume(token, "user", "", "digest") {
		t.Fatal("expired proof accepted")
	}
}

func TestTargetCredentialChangedIgnoresMetadataOnlyEdits(t *testing.T) {
	existing := targets.Target{Transport: targets.TransportSSH, Hostname: "host", IP: "10.0.0.1", Username: "demo", Auth: targets.PasswordAuth("secret")}
	request := UserTargetMutationRequest{
		Transport: existing.Transport, Hostname: existing.Hostname, IP: existing.IP,
		AuthType: existing.Auth.Type, Description: "new description",
	}
	if targetCredentialChanged(&existing, request) {
		t.Fatal("metadata-only edit requires credential test")
	}
	request.IP = "10.0.0.99"
	if !targetCredentialChanged(&existing, request) {
		t.Fatal("connection change did not require credential test")
	}
}
