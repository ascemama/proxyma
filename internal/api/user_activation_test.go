package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"proxyma/internal/users"
)

func TestUserActivationLifecycle(t *testing.T) {
	server, paths := testTargetMutationServer(t, nil)
	cookie, session := loginSession(t, server)
	create := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/users", cookie, session.CSRFToken, `{"username":"invitee","name":"Invited User","admin":false,"activation_token_lifetime_seconds":3600}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}
	var profile UserProfileResponse
	if err := json.Unmarshal(create.Body.Bytes(), &profile); err != nil {
		t.Fatal(err)
	}
	if profile.Status != users.StatusPendingActivation || profile.ActivationURL == "" {
		t.Fatalf("profile=%#v", profile)
	}
	token := activationTokenFromURL(t, profile.ActivationURL)

	login := loginRequest(t, server, `{"username":"invitee","password":"anything"}`)
	if login.Code != http.StatusUnauthorized || !strings.Contains(login.Body.String(), "invalid_login") {
		t.Fatalf("pending login=%d %s", login.Code, login.Body.String())
	}

	inspectReq := httptest.NewRequest(http.MethodGet, "/v1/user/activation", nil)
	inspectReq.Header.Set("Authorization", "Activation "+token)
	inspect := httptest.NewRecorder()
	server.Routes().ServeHTTP(inspect, inspectReq)
	if inspect.Code != http.StatusOK || !strings.Contains(inspect.Body.String(), "invitee") {
		t.Fatalf("inspect=%d %s", inspect.Code, inspect.Body.String())
	}

	completeReq := httptest.NewRequest(http.MethodPost, "/v1/user/activation", strings.NewReader(`{"new_password":"chosen-password"}`))
	completeReq.Header.Set("Authorization", "Activation "+token)
	complete := httptest.NewRecorder()
	server.Routes().ServeHTTP(complete, completeReq)
	if complete.Code != http.StatusOK {
		t.Fatalf("complete=%d %s", complete.Code, complete.Body.String())
	}
	stored, err := readStoredUser(paths, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != users.StatusActive || users.VerifyPassword([]byte("chosen-password"), stored.PasswdHash) != nil {
		t.Fatalf("stored=%#v", stored)
	}

	reuse := httptest.NewRecorder()
	server.Routes().ServeHTTP(reuse, completeReq.Clone(completeReq.Context()))
	if reuse.Code != http.StatusUnauthorized {
		t.Fatalf("reuse=%d %s", reuse.Code, reuse.Body.String())
	}
	activeLogin := loginRequest(t, server, `{"username":"invitee","password":"chosen-password"}`)
	if activeLogin.Code != http.StatusOK {
		t.Fatalf("active login=%d %s", activeLogin.Code, activeLogin.Body.String())
	}
}

func TestAdminPasswordResetLifecycle(t *testing.T) {
	server, paths := testTargetMutationServer(t, nil)
	adminCookie, adminSession := loginSession(t, server)
	hash, err := users.GeneratePasswordHash([]byte("old-password"))
	if err != nil {
		t.Fatal(err)
	}
	created, err := users.CreateUser(paths, users.User{Username: "reset-user", Name: "Reset User", Status: users.StatusActive, PasswdHash: hash}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.refreshUserRegistry(paths); err != nil {
		t.Fatal(err)
	}
	targetSession, targetCookie, _, err := server.UserSessions.Create(created.ID, time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	passwordEdit := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/users/"+created.ID, adminCookie, adminSession.CSRFToken, `{"username":"reset-user","name":"Reset User","password":"admin-selected"}`)
	if passwordEdit.Code != http.StatusBadRequest {
		t.Fatalf("password edit=%d %s", passwordEdit.Code, passwordEdit.Body.String())
	}
	withoutCSRF := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/users/"+created.ID+"/password-reset-link", adminCookie, "", `{}`)
	if withoutCSRF.Code != http.StatusForbidden {
		t.Fatalf("without csrf=%d %s", withoutCSRF.Code, withoutCSRF.Body.String())
	}
	tooLong := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/users/"+created.ID+"/password-reset-link", adminCookie, adminSession.CSRFToken, `{"activation_token_lifetime_seconds":172801}`)
	if tooLong.Code != http.StatusBadRequest {
		t.Fatalf("too long=%d %s", tooLong.Code, tooLong.Body.String())
	}

	reset := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/users/"+created.ID+"/password-reset-link", adminCookie, adminSession.CSRFToken, `{"activation_token_lifetime_seconds":3600}`)
	if reset.Code != http.StatusOK {
		t.Fatalf("reset=%d %s", reset.Code, reset.Body.String())
	}
	var first ActivationLinkResponse
	if err := json.Unmarshal(reset.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	firstToken := activationTokenFromURL(t, first.ActivationURL)
	stored, err := readStoredUser(paths, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != users.StatusPasswordResetPending || stored.PasswdHash != "" {
		t.Fatalf("stored=%#v", stored)
	}
	if _, err := server.UserSessions.Authenticate(targetCookie, time.Now()); !errors.Is(err, users.ErrSessionInvalid) {
		t.Fatalf("session %s err=%v", targetSession.SessionID, err)
	}
	if oldLogin := loginRequest(t, server, `{"username":"reset-user","password":"old-password"}`); oldLogin.Code != http.StatusUnauthorized {
		t.Fatalf("old login=%d %s", oldLogin.Code, oldLogin.Body.String())
	}

	regenerated := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/users/"+created.ID+"/password-reset-link", adminCookie, adminSession.CSRFToken, `{}`)
	if regenerated.Code != http.StatusOK {
		t.Fatalf("regenerate=%d %s", regenerated.Code, regenerated.Body.String())
	}
	var second ActivationLinkResponse
	if err := json.Unmarshal(regenerated.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	invalid := httptest.NewRequest(http.MethodGet, "/v1/user/activation", nil)
	invalid.Header.Set("Authorization", "Activation "+firstToken)
	invalidRec := httptest.NewRecorder()
	server.Routes().ServeHTTP(invalidRec, invalid)
	if invalidRec.Code != http.StatusUnauthorized {
		t.Fatalf("old link=%d %s", invalidRec.Code, invalidRec.Body.String())
	}
	secondToken := activationTokenFromURL(t, second.ActivationURL)
	inspect := httptest.NewRequest(http.MethodGet, "/v1/user/activation", nil)
	inspect.Header.Set("Authorization", "Activation "+secondToken)
	inspectRec := httptest.NewRecorder()
	server.Routes().ServeHTTP(inspectRec, inspect)
	if inspectRec.Code != http.StatusOK || !strings.Contains(inspectRec.Body.String(), `"purpose":"password_reset"`) {
		t.Fatalf("inspect=%d %s", inspectRec.Code, inspectRec.Body.String())
	}
	complete := httptest.NewRequest(http.MethodPost, "/v1/user/activation", strings.NewReader(`{"new_password":"new-password"}`))
	complete.Header.Set("Authorization", "Activation "+secondToken)
	completeRec := httptest.NewRecorder()
	server.Routes().ServeHTTP(completeRec, complete)
	if completeRec.Code != http.StatusOK || !strings.Contains(completeRec.Body.String(), `"status":"password_reset"`) {
		t.Fatalf("complete=%d %s", completeRec.Code, completeRec.Body.String())
	}
	if newLogin := loginRequest(t, server, `{"username":"reset-user","password":"new-password"}`); newLogin.Code != http.StatusOK {
		t.Fatalf("new login=%d %s", newLogin.Code, newLogin.Body.String())
	}

	self := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/users/"+adminSession.User.ID+"/password-reset-link", adminCookie, adminSession.CSRFToken, `{}`)
	if self.Code != http.StatusBadRequest || !strings.Contains(self.Body.String(), "self_password_reset") {
		t.Fatalf("self=%d %s", self.Code, self.Body.String())
	}
}

func TestActivationPolicyAndMaximumLifetime(t *testing.T) {
	server, _ := testTargetMutationServer(t, nil)
	cookie, session := loginSession(t, server)
	policy := userTargetMutationRequest(t, server, http.MethodGet, "/v1/user/activation-policy", cookie, session.CSRFToken, ``)
	if policy.Code != http.StatusOK || !strings.Contains(policy.Body.String(), `"maximum_lifetime_seconds":172800`) {
		t.Fatalf("policy=%d %s", policy.Code, policy.Body.String())
	}
	rejected := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/users", cookie, session.CSRFToken, `{"username":"too-long","name":"Too Long","activation_token_lifetime_seconds":172801}`)
	if rejected.Code != http.StatusBadRequest || !strings.Contains(rejected.Body.String(), "invalid_activation_lifetime") {
		t.Fatalf("rejected=%d %s", rejected.Code, rejected.Body.String())
	}
}

func activationTokenFromURL(t *testing.T, value string) string {
	t.Helper()
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	query, err := url.ParseQuery(strings.TrimPrefix(parsed.Fragment, "/activate?"))
	if err != nil {
		t.Fatal(err)
	}
	return query.Get("token")
}
