package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxyma/internal/agents"
	"proxyma/internal/ids"
	interactioninstances "proxyma/internal/interaction/instances"
	"proxyma/internal/sshclient"
	"proxyma/internal/targets"
	"proxyma/internal/taskflow/policy"
	"proxyma/internal/tasks"
	"proxyma/internal/users"
)

func TestLoginCreatesSecureUserSessionCookie(t *testing.T) {
	server := testLoginServer(t, false)
	rec := loginRequest(t, server, `{"username":"antoine","password":"correct-password"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response UserSessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.User.ID != testUser.ID || response.User.Username != "antoine" || response.User.Theme != users.ThemeLight || response.CSRFToken == "" {
		t.Fatalf("response = %#v", response)
	}
	if response.Build.Version != "1.2.3+4.gabcdef0" || response.Build.Commit != "abcdef0" {
		t.Fatalf("build = %#v", response.Build)
	}
	if strings.Contains(rec.Body.String(), "correct-password") || strings.Contains(rec.Body.String(), "passwd_hash") {
		t.Fatalf("login response leaked credential material: %s", rec.Body.String())
	}
	cookie := requireCookie(t, rec, userSessionCookieName)
	if !cookie.HttpOnly || !cookie.Secure || cookie.Path != "/" || cookie.Domain != "" || cookie.MaxAge <= 0 {
		t.Fatalf("cookie = %#v", cookie)
	}
	if !strings.Contains(rec.Header().Get("Set-Cookie"), "SameSite=Lax") {
		t.Fatalf("Set-Cookie missing SameSite=Lax: %s", rec.Header().Get("Set-Cookie"))
	}
}

func TestLoginUsesDevCookieWhenInsecureCookiesAllowed(t *testing.T) {
	server := testLoginServer(t, true)
	rec := loginRequest(t, server, `{"username":"antoine","password":"correct-password"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	cookie := requireCookie(t, rec, devUserSessionCookieName)
	if !cookie.HttpOnly || cookie.Secure || cookie.Path != "/" || cookie.Domain != "" {
		t.Fatalf("cookie = %#v", cookie)
	}
}

func TestLoginRejectsInvalidRequestsAndCredentials(t *testing.T) {
	server := testLoginServer(t, false)
	cases := []struct {
		name   string
		body   string
		status int
		code   string
	}{
		{name: "missing password", body: `{"username":"antoine"}`, status: http.StatusBadRequest},
		{name: "unknown field", body: `{"username":"antoine","password":"correct-password","extra":true}`, status: http.StatusBadRequest},
		{name: "wrong password", body: `{"username":"antoine","password":"wrong-password"}`, status: http.StatusUnauthorized, code: "invalid_login"},
		{name: "unknown user", body: `{"username":"missing","password":"wrong-password"}`, status: http.StatusUnauthorized, code: "invalid_login"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := loginRequest(t, server, tc.body)
			if rec.Code != tc.status {
				t.Fatalf("status = %d want %d body=%s", rec.Code, tc.status, rec.Body.String())
			}
			if tc.code != "" {
				var response ErrorResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if response.Code != tc.code {
					t.Fatalf("response = %#v", response)
				}
			}
			if strings.Contains(rec.Body.String(), "wrong-password") || strings.Contains(rec.Body.String(), "correct-password") {
				t.Fatalf("response leaked password: %s", rec.Body.String())
			}
		})
	}
}

func TestSessionRotatesCSRFAndLogoutRevokesSession(t *testing.T) {
	server := testLoginServer(t, false)
	loginRec := loginRequest(t, server, `{"username":"antoine","password":"correct-password"}`)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", loginRec.Code, loginRec.Body.String())
	}
	var loginResponse UserSessionResponse
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginResponse); err != nil {
		t.Fatal(err)
	}
	cookie := requireCookie(t, loginRec, userSessionCookieName)

	sessionReq := httptest.NewRequest(http.MethodGet, "/v1/user/session", nil)
	sessionReq.AddCookie(cookie)
	sessionRec := httptest.NewRecorder()
	server.Routes().ServeHTTP(sessionRec, sessionReq)
	if sessionRec.Code != http.StatusOK {
		t.Fatalf("session status = %d body=%s", sessionRec.Code, sessionRec.Body.String())
	}
	var sessionResponse UserSessionResponse
	if err := json.Unmarshal(sessionRec.Body.Bytes(), &sessionResponse); err != nil {
		t.Fatal(err)
	}
	if sessionResponse.CSRFToken == "" || sessionResponse.CSRFToken == loginResponse.CSRFToken {
		t.Fatalf("csrf was not rotated: login=%q session=%q", loginResponse.CSRFToken, sessionResponse.CSRFToken)
	}
	if sessionResponse.Build != loginResponse.Build {
		t.Fatalf("session build = %#v, login build = %#v", sessionResponse.Build, loginResponse.Build)
	}

	missingCSRFReq := httptest.NewRequest(http.MethodPost, "/v1/user/logout", nil)
	missingCSRFReq.AddCookie(cookie)
	missingCSRFRec := httptest.NewRecorder()
	server.Routes().ServeHTTP(missingCSRFRec, missingCSRFReq)
	if missingCSRFRec.Code != http.StatusForbidden {
		t.Fatalf("missing csrf status = %d body=%s", missingCSRFRec.Code, missingCSRFRec.Body.String())
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/v1/user/logout", nil)
	logoutReq.AddCookie(cookie)
	logoutReq.Header.Set(userSessionCSRFHeader, sessionResponse.CSRFToken)
	logoutRec := httptest.NewRecorder()
	server.Routes().ServeHTTP(logoutRec, logoutReq)
	if logoutRec.Code != http.StatusOK {
		t.Fatalf("logout status = %d body=%s", logoutRec.Code, logoutRec.Body.String())
	}
	clearCookie := requireCookie(t, logoutRec, userSessionCookieName)
	if clearCookie.MaxAge >= 0 {
		t.Fatalf("clear cookie = %#v", clearCookie)
	}

	afterLogoutReq := httptest.NewRequest(http.MethodGet, "/v1/user/session", nil)
	afterLogoutReq.AddCookie(cookie)
	afterLogoutRec := httptest.NewRecorder()
	server.Routes().ServeHTTP(afterLogoutRec, afterLogoutReq)
	if afterLogoutRec.Code != http.StatusUnauthorized {
		t.Fatalf("after logout status = %d body=%s", afterLogoutRec.Code, afterLogoutRec.Body.String())
	}
}

func TestChangePasswordValidatesAuthenticationCSRFAndInput(t *testing.T) {
	server, _ := testTargetMutationServer(t, nil)
	cookie, session := loginSession(t, server)

	cases := []struct {
		name   string
		cookie *http.Cookie
		csrf   string
		body   string
		status int
	}{
		{name: "unauthenticated", body: `{}`, status: http.StatusUnauthorized},
		{name: "missing csrf", cookie: cookie, body: `{"current_password":"correct-password","new_password":"new-password"}`, status: http.StatusForbidden},
		{name: "malformed", cookie: cookie, csrf: session.CSRFToken, body: `{`, status: http.StatusBadRequest},
		{name: "unknown field", cookie: cookie, csrf: session.CSRFToken, body: `{"current_password":"correct-password","new_password":"new-password","confirm_password":"new-password"}`, status: http.StatusBadRequest},
		{name: "missing current", cookie: cookie, csrf: session.CSRFToken, body: `{"new_password":"new-password"}`, status: http.StatusBadRequest},
		{name: "missing new", cookie: cookie, csrf: session.CSRFToken, body: `{"current_password":"correct-password"}`, status: http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/change-password", tc.cookie, tc.csrf, tc.body)
			if rec.Code != tc.status {
				t.Fatalf("status = %d want %d body=%s", rec.Code, tc.status, rec.Body.String())
			}
		})
	}
}

func TestChangePasswordRejectsWrongCurrentPasswordAndThrottles(t *testing.T) {
	server, paths := testTargetMutationServer(t, nil)
	cookie, session := loginSession(t, server)
	body := `{"current_password":"wrong-password","new_password":"new-password"}`
	for attempt := 0; attempt < maxLoginFailures; attempt++ {
		rec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/change-password", cookie, session.CSRFToken, body)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d body=%s", attempt+1, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "wrong-password") || strings.Contains(rec.Body.String(), "new-password") {
			t.Fatalf("response leaked password: %s", rec.Body.String())
		}
	}
	throttled := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/change-password", cookie, session.CSRFToken, body)
	if throttled.Code != http.StatusTooManyRequests {
		t.Fatalf("throttled status = %d body=%s", throttled.Code, throttled.Body.String())
	}
	var response ErrorResponse
	if err := json.Unmarshal(throttled.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code != "password_change_throttled" || !response.Retryable {
		t.Fatalf("response = %#v", response)
	}
	stored, err := readStoredUser(paths, testUser.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := users.VerifyPassword([]byte("correct-password"), stored.PasswdHash); err != nil {
		t.Fatalf("password changed after rejected attempts: %v", err)
	}
}

func TestChangePasswordUpdatesPasswordRevokesOtherSessionsAndLogs(t *testing.T) {
	server, paths := testTargetMutationServer(t, nil)
	var logs bytes.Buffer
	server.Logger = log.New(&logs, "", 0)
	cookie, session := loginSession(t, server)
	_, otherCookieValue, _, err := server.UserSessions.Create(testUser.ID, time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	otherUserID := "a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5"
	_, differentUserCookie, _, err := server.UserSessions.Create(otherUserID, time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	body := `{"current_password":"correct-password","new_password":"new-password"}`
	rec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/change-password", cookie, session.CSRFToken, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	stored, err := readStoredUser(paths, testUser.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := users.VerifyPassword([]byte("new-password"), stored.PasswdHash); err != nil {
		t.Fatalf("new password did not verify: %v", err)
	}
	if err := users.VerifyPassword([]byte("correct-password"), stored.PasswdHash); err == nil {
		t.Fatal("old password still verifies")
	}

	currentSession := httptest.NewRequest(http.MethodGet, "/v1/user/session", nil)
	currentSession.AddCookie(cookie)
	currentRec := httptest.NewRecorder()
	server.Routes().ServeHTTP(currentRec, currentSession)
	if currentRec.Code != http.StatusOK {
		t.Fatalf("current session status = %d body=%s", currentRec.Code, currentRec.Body.String())
	}
	if _, err := server.UserSessions.Authenticate(otherCookieValue, time.Now()); err != users.ErrSessionInvalid {
		t.Fatalf("other session err = %v, want %v", err, users.ErrSessionInvalid)
	}
	if _, err := server.UserSessions.Authenticate(differentUserCookie, time.Now()); err != nil {
		t.Fatalf("different user's session was revoked: %v", err)
	}

	oldLogin := loginRequest(t, server, `{"username":"antoine","password":"correct-password"}`)
	if oldLogin.Code != http.StatusUnauthorized {
		t.Fatalf("old login status = %d body=%s", oldLogin.Code, oldLogin.Body.String())
	}
	newLogin := loginRequest(t, server, `{"username":"antoine","password":"new-password"}`)
	if newLogin.Code != http.StatusOK {
		t.Fatalf("new login status = %d body=%s", newLogin.Code, newLogin.Body.String())
	}
	logText := logs.String()
	for _, expected := range []string{"user password changed", testUser.ID, `username="antoine"`, "revoked_sessions=1"} {
		if !strings.Contains(logText, expected) {
			t.Fatalf("log missing %q: %s", expected, logText)
		}
	}
	for _, secret := range []string{"correct-password", "new-password", stored.PasswdHash} {
		if strings.Contains(logText, secret) {
			t.Fatalf("log leaked credential material %q: %s", secret, logText)
		}
	}
}

func TestUserSettingsValidatesAuthenticationCSRFAndTheme(t *testing.T) {
	server, _ := testTargetMutationServer(t, nil)
	cookie, session := loginSession(t, server)
	cases := []struct {
		name   string
		cookie *http.Cookie
		csrf   string
		body   string
		status int
	}{
		{name: "unauthenticated", body: `{"theme":"dark"}`, status: http.StatusUnauthorized},
		{name: "missing csrf", cookie: cookie, body: `{"theme":"dark"}`, status: http.StatusForbidden},
		{name: "malformed", cookie: cookie, csrf: session.CSRFToken, body: `{`, status: http.StatusBadRequest},
		{name: "unknown field", cookie: cookie, csrf: session.CSRFToken, body: `{"theme":"dark","extra":true}`, status: http.StatusBadRequest},
		{name: "missing theme", cookie: cookie, csrf: session.CSRFToken, body: `{}`, status: http.StatusBadRequest},
		{name: "unsupported theme", cookie: cookie, csrf: session.CSRFToken, body: `{"theme":"system"}`, status: http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/settings", tc.cookie, tc.csrf, tc.body)
			if rec.Code != tc.status {
				t.Fatalf("status = %d want %d body=%s", rec.Code, tc.status, rec.Body.String())
			}
		})
	}
}

func TestUserSettingsPersistsThemeInLegacyRecordAndSession(t *testing.T) {
	server, paths := testTargetMutationServer(t, nil)
	idPath := filepath.Join(paths.UserDir, testUser.ID+".json")
	legacyPath := filepath.Join(paths.UserDir, "antoine.json")
	if err := os.Rename(idPath, legacyPath); err != nil {
		t.Fatal(err)
	}
	cookie, session := loginSession(t, server)
	rec := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/settings", cookie, session.CSRFToken, `{"theme":"dark"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response UserProfileResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Theme != users.ThemeDark {
		t.Fatalf("response = %#v", response)
	}
	var stored users.User
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Theme != users.ThemeDark {
		t.Fatalf("stored theme = %q", stored.Theme)
	}
	if _, err := os.Stat(idPath); !os.IsNotExist(err) {
		t.Fatalf("unexpected ID-named user record: %v", err)
	}

	sessionReq := httptest.NewRequest(http.MethodGet, "/v1/user/session", nil)
	sessionReq.AddCookie(cookie)
	sessionRec := httptest.NewRecorder()
	server.Routes().ServeHTTP(sessionRec, sessionReq)
	if sessionRec.Code != http.StatusOK {
		t.Fatalf("session status = %d body=%s", sessionRec.Code, sessionRec.Body.String())
	}
	var sessionResponse UserSessionResponse
	if err := json.Unmarshal(sessionRec.Body.Bytes(), &sessionResponse); err != nil {
		t.Fatal(err)
	}
	if sessionResponse.User.Theme != users.ThemeDark {
		t.Fatalf("session response = %#v", sessionResponse)
	}
}

func TestUserAgentsEndpointAdminSeesAllAgentsAndRedactsSecrets(t *testing.T) {
	server := testLoginServer(t, false)
	owned := agents.Agent{
		ID:           testAgent.ID,
		DisplayName:  "Owned Agent",
		SharedSecret: "owned-agent-secret",
		Enabled:      true,
		UserIDs:      []string{testUser.ID},
	}
	other := agents.Agent{
		ID:           "7937dd2d-0b8a-4880-8afa-0ea60fd65a43",
		DisplayName:  "Other Agent",
		SharedSecret: "other-agent-secret",
		Enabled:      true,
		UserIDs:      []string{"a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5"},
	}
	shared := agents.Agent{
		ID:           "8dc60324-b3bb-4d19-9aa6-0e149e4938b8",
		DisplayName:  "Shared Agent",
		SharedSecret: "shared-agent-secret",
		Enabled:      false,
		UserIDs:      []string{"a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5", testUser.ID},
	}
	server.Agents = agents.NewRegistry([]agents.Agent{owned, other, shared})
	cookie := loginCookie(t, server)

	req := httptest.NewRequest(http.MethodGet, "/v1/user/agents", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response UserAgentsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Agents) != 3 || response.Agents[0].ID != other.ID || response.Agents[1].ID != shared.ID || response.Agents[2].ID != owned.ID {
		t.Fatalf("response = %#v", response)
	}
	body := rec.Body.String()
	for _, leaked := range []string{"owned-agent-secret", "shared-agent-secret", "other-agent-secret"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("agent response leaked secret %q: %s", leaked, body)
		}
	}
}

func TestUserAgentsEndpointNonAdminFiltersOwnedAgents(t *testing.T) {
	server := testLoginServer(t, false)
	currentUser, found := server.userRegistry().Find(testUser.ID)
	if !found {
		t.Fatal("missing current user")
	}
	currentUser.Admin = false
	server.setUserRegistry(users.NewRegistry([]users.User{currentUser}))
	owned := agents.Agent{
		ID:           testAgent.ID,
		DisplayName:  "Owned Agent",
		SharedSecret: "owned-agent-secret",
		Enabled:      true,
		UserIDs:      []string{testUser.ID},
	}
	other := agents.Agent{
		ID:           "7937dd2d-0b8a-4880-8afa-0ea60fd65a43",
		DisplayName:  "Other Agent",
		SharedSecret: "other-agent-secret",
		Enabled:      true,
		UserIDs:      []string{"a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5"},
	}
	shared := agents.Agent{
		ID:           "8dc60324-b3bb-4d19-9aa6-0e149e4938b8",
		DisplayName:  "Shared Agent",
		SharedSecret: "shared-agent-secret",
		Enabled:      false,
		UserIDs:      []string{"a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5", testUser.ID},
	}
	server.Agents = agents.NewRegistry([]agents.Agent{owned, other, shared})
	cookie := loginCookie(t, server)

	req := httptest.NewRequest(http.MethodGet, "/v1/user/agents", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response UserAgentsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Agents) != 2 || response.Agents[0].ID != shared.ID || response.Agents[1].ID != owned.ID {
		t.Fatalf("response = %#v", response)
	}
}

func TestUserTargetsEndpointAdminSeesAllTargetsAndRedactsCredentials(t *testing.T) {
	server := testLoginServer(t, false)
	owned := targets.Target{
		ID:          "8dc60324-b3bb-4d19-9aa6-0e149e4938b8",
		Hostname:    "owned.example",
		IP:          "10.0.0.1",
		Username:    "owned-user",
		PolicyIDs:   []string{"baseline"},
		AgentIDs:    []string{testAgent.ID},
		UserIDs:     []string{testUser.ID},
		Auth:        targets.PasswordAuth("owned-target-password"),
		Description: "owned target",
	}
	other := targets.Target{
		ID:        "7937dd2d-0b8a-4880-8afa-0ea60fd65a43",
		Hostname:  "other.example",
		Username:  "other-user",
		PolicyIDs: []string{"6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"},
		AgentIDs:  []string{testAgent.ID},
		UserIDs:   []string{"a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5"},
		Auth:      targets.PasswordAuth("other-target-password"),
	}
	shared := targets.Target{
		ID:        "a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5",
		Hostname:  "shared.example",
		Username:  "shared-user",
		PolicyIDs: []string{"6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"},
		AgentIDs:  []string{testAgent.ID},
		UserIDs:   []string{"7937dd2d-0b8a-4880-8afa-0ea60fd65a43", testUser.ID},
		Auth:      targets.PrivateKeyAuth("secrets/private_keys/shared-key"),
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
		PolicyIDs:   []string{"baseline"},
		AgentIDs:    []string{testAgent.ID},
		UserIDs:     []string{testUser.ID},
		Auth:        targets.PasswordAuth("owned-target-password"),
		Description: "owned target",
	}
	other := targets.Target{
		ID:        "7937dd2d-0b8a-4880-8afa-0ea60fd65a43",
		Hostname:  "other.example",
		Username:  "other-user",
		PolicyIDs: []string{"6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"},
		AgentIDs:  []string{testAgent.ID},
		UserIDs:   []string{"a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5"},
		Auth:      targets.PasswordAuth("other-target-password"),
	}
	shared := targets.Target{
		ID:        "a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5",
		Hostname:  "shared.example",
		Username:  "shared-user",
		PolicyIDs: []string{"6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"},
		AgentIDs:  []string{testAgent.ID},
		UserIDs:   []string{"7937dd2d-0b8a-4880-8afa-0ea60fd65a43", testUser.ID},
		Auth:      targets.PrivateKeyAuth("secrets/private_keys/shared-key"),
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
		"policy_ids":["6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"],
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
		"policy_ids":["6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"],
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
		"policy_ids":["6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"],
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
		"policy_ids":  {"6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"},
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
		"hostname":   {"KEY-SRV-EMPTY"},
		"username":   {"fixture-key"},
		"policy_ids": {"6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"},
		"agent_ids":  {testAgent.ID},
		"auth_type":  {targets.AuthTypePrivateKey},
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
			ID:        "8dc60324-b3bb-4d19-9aa6-0e149e4938b8",
			Hostname:  "shared.example",
			IP:        "10.10.1.95",
			Username:  "first-user",
			PolicyIDs: []string{"6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"},
			AgentIDs:  []string{testAgent.ID},
			UserIDs:   []string{testUser.ID},
			Auth:      targets.PasswordAuth("first-secret"),
		},
	})
	cookie, session := loginSession(t, server)
	createBody := `{
		"hostname":"shared.example",
		"ip":"10.10.1.95",
		"description":"Same VM with a different credential",
		"username":"second-user",
		"policy_ids":["6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"],
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
			ID:        foreignID,
			Hostname:  "foreign.example",
			Username:  "foreign-user",
			PolicyIDs: []string{"6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"},
			AgentIDs:  []string{testAgent.ID},
			UserIDs:   []string{"a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5"},
			Auth:      targets.PasswordAuth("foreign-secret"),
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
			ID:        foreignID,
			Hostname:  "foreign.example",
			Username:  "foreign-user",
			PolicyIDs: []string{"6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"},
			AgentIDs:  []string{testAgent.ID},
			UserIDs:   []string{"a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5"},
			Auth:      targets.PasswordAuth("foreign-secret"),
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

func TestUserPoliciesEndpointNonAdminSeesAllPolicies(t *testing.T) {
	server, _ := testTargetMutationServer(t, nil)
	currentUser, found := server.userRegistry().Find(testUser.ID)
	if !found {
		t.Fatal("missing test user")
	}
	currentUser.Admin = false
	server.setUserRegistry(users.NewRegistry([]users.User{currentUser}))
	cookie, _ := loginSession(t, server)

	rec := userTargetMutationRequest(t, server, http.MethodGet, "/v1/user/policies", cookie, "", ``)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response UserPoliciesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Policies) != 1 || response.Policies[0].ID != "6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c" {
		t.Fatalf("policies = %#v", response.Policies)
	}
}

func TestUserPolicyMutationsCreateRejectInvalidAndDisallowEdit(t *testing.T) {
	server, _ := testTargetMutationServer(t, nil)
	cookie, session := loginSession(t, server)

	createBody := `{
		"name":"Runtime allow policy",
		"description":"Created by API test",
		"priority":42,
		"enabled":true,
		"rules":[{"id":"allow-hostname","effect":"allow","remote_command_prefix_any":["hostname"],"reason":"test"}]
	}`
	createRec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/policies", cookie, session.CSRFToken, createBody)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created UserPolicyResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Name != "Runtime allow policy" || len(created.Rules) != 1 {
		t.Fatalf("created = %#v", created)
	}
	result, err := server.Policies.Evaluate(context.Background(), policy.Request{PolicyIDs: []string{created.ID}, RemoteCommand: "hostname"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != policy.DecisionAllow {
		t.Fatalf("policy decision = %#v", result)
	}

	invalid := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/policies", cookie, session.CSRFToken, `{"name":"Broken"}`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid status = %d body=%s", invalid.Code, invalid.Body.String())
	}

	edit := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/policies/"+created.ID, cookie, session.CSRFToken, `{"name":"Edit"}`)
	if edit.Code != http.StatusMethodNotAllowed {
		t.Fatalf("edit status = %d body=%s", edit.Code, edit.Body.String())
	}
}

func TestUserPolicyDeleteRejectsReferencedPolicyAndRefreshesEvaluator(t *testing.T) {
	referencedTarget := targets.Target{
		ID:        "8dc60324-b3bb-4d19-9aa6-0e149e4938b8",
		Hostname:  "referenced.example",
		Username:  "fixture",
		PolicyIDs: []string{"6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"},
		AgentIDs:  []string{testAgent.ID},
		UserIDs:   []string{testUser.ID},
		Auth:      targets.PasswordAuth("secret"),
	}
	server, _ := testTargetMutationServer(t, []targets.Target{referencedTarget})
	cookie, session := loginSession(t, server)

	referencedDelete := userTargetMutationRequest(t, server, http.MethodDelete, "/v1/user/policies/6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c", cookie, session.CSRFToken, ``)
	if referencedDelete.Code != http.StatusBadRequest {
		t.Fatalf("referenced delete status = %d body=%s", referencedDelete.Code, referencedDelete.Body.String())
	}

	createBody := `{"name":"Temporary policy","rules":[{"id":"allow-all","effect":"allow","always":true}]}`
	createRec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/policies", cookie, session.CSRFToken, createBody)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created UserPolicyResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	deleteRec := userTargetMutationRequest(t, server, http.MethodDelete, "/v1/user/policies/"+created.ID, cookie, session.CSRFToken, ``)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
	result, err := server.Policies.Evaluate(context.Background(), policy.Request{PolicyIDs: []string{created.ID}, RemoteCommand: "hostname"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != policy.DecisionDeny || !strings.Contains(result.Reason, "unknown policy") {
		t.Fatalf("policy decision after delete = %#v", result)
	}
}

func TestUserAgentMutationsCreateUpdateDeleteOwnedAgent(t *testing.T) {
	server, paths := testTargetMutationServer(t, nil)
	cookie, session := loginSession(t, server)

	createBody := `{
		"display_name":"Build Agent",
		"shared_secret":"build-agent-secret",
		"enabled":true
	}`
	createRec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/agents", cookie, session.CSRFToken, createBody)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	if strings.Contains(createRec.Body.String(), "build-agent-secret") {
		t.Fatalf("create response leaked shared secret: %s", createRec.Body.String())
	}
	var created UserAgentResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.DisplayName != "Build Agent" || !created.Enabled {
		t.Fatalf("created response = %#v", created)
	}
	stored, err := readStoredAgent(paths, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.SharedSecret != "build-agent-secret" || len(stored.UserIDs) != 1 || stored.UserIDs[0] != testUser.ID || stored.InteractionInstanceID != "local" {
		t.Fatalf("stored agent = %#v", stored)
	}

	updateBody := `{
		"display_name":"Updated Build Agent",
		"enabled":false
	}`
	updateRec := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/agents/"+created.ID, cookie, session.CSRFToken, updateBody)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", updateRec.Code, updateRec.Body.String())
	}
	updated, err := readStoredAgent(paths, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.DisplayName != "Updated Build Agent" || updated.Enabled {
		t.Fatalf("updated agent metadata = %#v", updated)
	}
	if updated.SharedSecret != "build-agent-secret" {
		t.Fatalf("update did not preserve shared secret = %#v", updated)
	}
	if refreshed, found := server.agentRegistry().Find(created.ID); !found || refreshed.DisplayName != "Updated Build Agent" {
		t.Fatalf("refreshed registry agent = %#v found=%t", refreshed, found)
	}

	deleteRec := userTargetMutationRequest(t, server, http.MethodDelete, "/v1/user/agents/"+created.ID, cookie, session.CSRFToken, ``)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
	if _, err := readStoredAgent(paths, created.ID); !os.IsNotExist(err) {
		t.Fatalf("expected deleted agent file, err=%v", err)
	}
	if _, found := server.agentRegistry().Find(created.ID); found {
		t.Fatalf("refreshed registry still contained deleted agent")
	}
}

func TestUserAgentInteractionAssignmentsRequireCurrentUserOwnership(t *testing.T) {
	server, paths := testInteractionMutationServer(t)
	ownedID := "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8"
	foreignID := "7937dd2d-0b8a-4880-8afa-0ea60fd65a43"
	disabledID := "1f8bd68e-bbf2-43ed-a89f-2c3cd741f32a"
	foreignUserID := "a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5"
	writeTestNtfyInteraction(t, paths.InteractionConfigDir, "owned.json", ownedID, testUser.ID, "Owned ntfy", "")
	writeTestNtfyInteraction(t, paths.InteractionConfigDir, "foreign.json", foreignID, foreignUserID, "Foreign ntfy", "")
	writeTestNtfyInteraction(t, paths.InteractionConfigDir, "disabled.json", disabledID, testUser.ID, "Disabled ntfy", "")
	_, disabled, err := interactioninstances.FindNtfy(paths.InteractionConfigDir, disabledID)
	if err != nil {
		t.Fatal(err)
	}
	disabled.Enabled = false
	if _, err := interactioninstances.UpdateNtfy(paths.InteractionConfigDir, disabledID, disabled); err != nil {
		t.Fatal(err)
	}

	cookie, session := loginSession(t, server)
	create := func(instanceID string) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"shared_secret":"assignment-secret","interaction_instance_id":%q}`, instanceID)
		return userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/agents", cookie, session.CSRFToken, body)
	}

	ownedRec := create(ownedID)
	if ownedRec.Code != http.StatusCreated {
		t.Fatalf("owned assignment status = %d body=%s", ownedRec.Code, ownedRec.Body.String())
	}
	var created UserAgentResponse
	if err := json.Unmarshal(ownedRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.InteractionInstanceID != ownedID {
		t.Fatalf("created interaction instance = %q", created.InteractionInstanceID)
	}

	for name, instanceID := range map[string]string{
		"foreign":  foreignID,
		"disabled": disabledID,
		"unknown":  "0c0591f0-b77c-45af-af3a-bca6c4ea04ee",
	} {
		t.Run(name, func(t *testing.T) {
			rec := create(instanceID)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestUserAgentInteractionAssignmentPreservesLegacyUntilReplaced(t *testing.T) {
	server, paths := testInteractionMutationServer(t)
	ownedID := "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8"
	foreignID := "7937dd2d-0b8a-4880-8afa-0ea60fd65a43"
	writeTestNtfyInteraction(t, paths.InteractionConfigDir, "owned.json", ownedID, testUser.ID, "Owned ntfy", "")
	writeTestNtfyInteraction(t, paths.InteractionConfigDir, "foreign.json", foreignID, "a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5", "Foreign ntfy", "")

	legacy := testAgent
	legacy.UserIDs = []string{testUser.ID}
	legacy.InteractionInstanceID = foreignID
	writeJSONRecord(t, paths.AgentDir, legacy.ID, legacy)
	server.setAgentRegistry(agents.NewRegistry([]agents.Agent{legacy}))
	cookie, session := loginSession(t, server)

	omitted := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/agents/"+legacy.ID, cookie, session.CSRFToken, `{"display_name":"Legacy assignment"}`)
	if omitted.Code != http.StatusOK {
		t.Fatalf("omitted status = %d body=%s", omitted.Code, omitted.Body.String())
	}
	unchanged := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/agents/"+legacy.ID, cookie, session.CSRFToken, fmt.Sprintf(`{"interaction_instance_id":%q}`, foreignID))
	if unchanged.Code != http.StatusOK {
		t.Fatalf("unchanged status = %d body=%s", unchanged.Code, unchanged.Body.String())
	}
	replaced := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/agents/"+legacy.ID, cookie, session.CSRFToken, fmt.Sprintf(`{"interaction_instance_id":%q}`, ownedID))
	if replaced.Code != http.StatusOK {
		t.Fatalf("replacement status = %d body=%s", replaced.Code, replaced.Body.String())
	}
	reselectForeign := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/agents/"+legacy.ID, cookie, session.CSRFToken, fmt.Sprintf(`{"interaction_instance_id":%q}`, foreignID))
	if reselectForeign.Code != http.StatusBadRequest {
		t.Fatalf("foreign reselection status = %d body=%s", reselectForeign.Code, reselectForeign.Body.String())
	}
}

func TestUserInteractionMutationRejectsDisablingReferencedInstance(t *testing.T) {
	server, paths := testInteractionMutationServer(t)
	instanceID := "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8"
	writeTestNtfyInteraction(t, paths.InteractionConfigDir, "owned.json", instanceID, testUser.ID, "Owned ntfy", "")
	referencingAgent := testAgent
	referencingAgent.UserIDs = []string{testUser.ID}
	referencingAgent.InteractionInstanceID = instanceID
	server.setAgentRegistry(agents.NewRegistry([]agents.Agent{referencingAgent}))
	cookie, session := loginSession(t, server)

	rec := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/interactions/"+instanceID, cookie, session.CSRFToken, `{"enabled":false}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "interaction_referenced") {
		t.Fatalf("disable referenced status = %d body=%s", rec.Code, rec.Body.String())
	}
	_, stored, err := interactioninstances.FindNtfy(paths.InteractionConfigDir, instanceID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Enabled {
		t.Fatal("referenced interaction was disabled")
	}
}

func TestUserAgentMutationsRejectForeignAndReferencedAgent(t *testing.T) {
	foreignID := "7937dd2d-0b8a-4880-8afa-0ea60fd65a43"
	server, paths := testTargetMutationServer(t, nil)
	currentUser, found := server.userRegistry().Find(testUser.ID)
	if !found {
		t.Fatal("missing current user")
	}
	currentUser.Admin = false
	server.setUserRegistry(users.NewRegistry([]users.User{currentUser}))
	foreignAgent := agents.Agent{
		ID:           foreignID,
		DisplayName:  "Foreign Agent",
		SharedSecret: "foreign-agent-secret",
		Enabled:      true,
		UserIDs:      []string{"a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5"},
	}
	writeJSONRecord(t, paths.AgentDir, foreignAgent.ID, foreignAgent)
	server.setAgentRegistry(agents.NewRegistry([]agents.Agent{testAgent, foreignAgent}))

	unauthenticated := httptest.NewRecorder()
	server.Routes().ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodPost, "/v1/user/agents", strings.NewReader(`{}`)))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d body=%s", unauthenticated.Code, unauthenticated.Body.String())
	}

	cookie, session := loginSession(t, server)
	invalid := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/agents", cookie, session.CSRFToken, `{"shared_secret":""}`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid status = %d body=%s", invalid.Code, invalid.Body.String())
	}

	missingCSRF := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/agents", cookie, "", `{"shared_secret":"secret"}`)
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatalf("missing csrf status = %d body=%s", missingCSRF.Code, missingCSRF.Body.String())
	}

	updateForeign := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/agents/"+foreignID, cookie, session.CSRFToken, `{"display_name":"Taken"}`)
	if updateForeign.Code != http.StatusForbidden {
		t.Fatalf("foreign update status = %d body=%s", updateForeign.Code, updateForeign.Body.String())
	}
	deleteForeign := userTargetMutationRequest(t, server, http.MethodDelete, "/v1/user/agents/"+foreignID, cookie, session.CSRFToken, ``)
	if deleteForeign.Code != http.StatusForbidden {
		t.Fatalf("foreign delete status = %d body=%s", deleteForeign.Code, deleteForeign.Body.String())
	}

	createRec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/agents", cookie, session.CSRFToken, `{"shared_secret":"referenced-secret"}`)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create referenced status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created UserAgentResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	writeJSONRecord(t, paths.TargetDir, "d252397d-7bd9-41b1-97c7-245ff04fdd33", targets.Target{
		ID:        "d252397d-7bd9-41b1-97c7-245ff04fdd33",
		Transport: targets.TransportSSH,
		OS:        targets.OSLinux,
		Hostname:  "referenced-agent.example",
		Username:  "fixture-user",
		PolicyIDs: []string{"6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"},
		AgentIDs:  []string{created.ID},
		UserIDs:   []string{testUser.ID},
		Auth:      targets.PasswordAuth("target-secret"),
	})
	deleteReferenced := userTargetMutationRequest(t, server, http.MethodDelete, "/v1/user/agents/"+created.ID, cookie, session.CSRFToken, ``)
	if deleteReferenced.Code != http.StatusBadRequest {
		t.Fatalf("referenced delete status = %d body=%s", deleteReferenced.Code, deleteReferenced.Body.String())
	}
}

func TestUserMutationsListCreateUpdateDeleteUser(t *testing.T) {
	server, paths := testTargetMutationServer(t, nil)
	cookie, session := loginSession(t, server)

	listRec := userTargetMutationRequest(t, server, http.MethodGet, "/v1/user/users", cookie, "", ``)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	if strings.Contains(listRec.Body.String(), testUser.PasswdHash) || strings.Contains(listRec.Body.String(), "passwd_hash") {
		t.Fatalf("list response leaked password hash: %s", listRec.Body.String())
	}

	createBody := `{
		"username":"ops-user",
		"name":"Operations User",
		"admin":false
	}`
	createRec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/users", cookie, session.CSRFToken, createBody)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	if strings.Contains(createRec.Body.String(), "passwd_hash") {
		t.Fatalf("create response leaked password data: %s", createRec.Body.String())
	}
	var created UserProfileResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Username != "ops-user" || created.Name != "Operations User" || created.Admin || created.Status != users.StatusPendingActivation || created.ActivationURL == "" {
		t.Fatalf("created response = %#v", created)
	}
	stored, err := readStoredUser(paths, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.PasswdHash != "" || stored.Status != users.StatusPendingActivation {
		t.Fatalf("stored pending user = %#v", stored)
	}
	originalHash := stored.PasswdHash

	updateBody := `{
		"username":"ops-user-2",
		"name":"Operations User 2",
		"admin":true
	}`
	updateRec := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/users/"+created.ID, cookie, session.CSRFToken, updateBody)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", updateRec.Code, updateRec.Body.String())
	}
	updated, err := readStoredUser(paths, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Username != "ops-user-2" || updated.Name != "Operations User 2" || !updated.Admin {
		t.Fatalf("updated user metadata = %#v", updated)
	}
	if updated.PasswdHash != originalHash {
		t.Fatalf("blank password update did not preserve hash")
	}
	if refreshed, found := server.userRegistry().Find(created.ID); !found || refreshed.Username != "ops-user-2" {
		t.Fatalf("refreshed registry user = %#v found=%t", refreshed, found)
	}

	deleteRec := userTargetMutationRequest(t, server, http.MethodDelete, "/v1/user/users/"+created.ID, cookie, session.CSRFToken, ``)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
	if _, err := readStoredUser(paths, created.ID); !os.IsNotExist(err) {
		t.Fatalf("expected deleted user file, err=%v", err)
	}
	if _, found := server.userRegistry().Find(created.ID); found {
		t.Fatalf("refreshed registry still contained deleted user")
	}
}

func TestUserMutationsRejectNonAdminAndLockoutRequests(t *testing.T) {
	server, paths := testTargetMutationServer(t, nil)
	adminUser, found := server.userRegistry().Find(testUser.ID)
	if !found {
		t.Fatal("missing admin user")
	}
	nonAdmin := users.User{
		ID:         "a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5",
		Username:   "operator",
		Name:       "Operator",
		Admin:      false,
		Status:     users.StatusActive,
		PasswdHash: adminUser.PasswdHash,
	}
	writeJSONRecord(t, paths.UserDir, nonAdmin.ID, nonAdmin)
	server.setUserRegistry(users.NewRegistry([]users.User{adminUser, nonAdmin}))

	nonAdminLogin := loginRequest(t, server, `{"username":"operator","password":"correct-password"}`)
	if nonAdminLogin.Code != http.StatusOK {
		t.Fatalf("non-admin login status = %d body=%s", nonAdminLogin.Code, nonAdminLogin.Body.String())
	}
	nonAdminCookie := requireCookie(t, nonAdminLogin, userSessionCookieName)
	var nonAdminSession UserSessionResponse
	if err := json.Unmarshal(nonAdminLogin.Body.Bytes(), &nonAdminSession); err != nil {
		t.Fatal(err)
	}
	for _, request := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/v1/user/users", ``},
		{http.MethodPost, "/v1/user/users", `{"username":"blocked","name":"Blocked","password":"password"}`},
		{http.MethodPut, "/v1/user/users/" + testUser.ID, `{"username":"antoine","name":"Antoine","admin":true}`},
		{http.MethodDelete, "/v1/user/users/" + testUser.ID, ``},
	} {
		rec := userTargetMutationRequest(t, server, request.method, request.path, nonAdminCookie, nonAdminSession.CSRFToken, request.body)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s status = %d body=%s", request.method, request.path, rec.Code, rec.Body.String())
		}
	}

	adminCookie, adminSession := loginSession(t, server)
	selfDelete := userTargetMutationRequest(t, server, http.MethodDelete, "/v1/user/users/"+testUser.ID, adminCookie, adminSession.CSRFToken, ``)
	if selfDelete.Code != http.StatusBadRequest {
		t.Fatalf("self delete status = %d body=%s", selfDelete.Code, selfDelete.Body.String())
	}
	selfDemote := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/users/"+testUser.ID, adminCookie, adminSession.CSRFToken, `{"username":"antoine","name":"Antoine","admin":false}`)
	if selfDemote.Code != http.StatusBadRequest {
		t.Fatalf("self demote status = %d body=%s", selfDemote.Code, selfDemote.Body.String())
	}
}

func TestUserMutationsRejectReferencedUserDelete(t *testing.T) {
	server, paths := testTargetMutationServer(t, nil)
	cookie, session := loginSession(t, server)
	createRec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/users", cookie, session.CSRFToken, `{
		"username":"owned-agent-user",
		"name":"Owned Agent User",
		"admin":false
	}`)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created UserProfileResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	writeJSONRecord(t, paths.AgentDir, "d252397d-7bd9-41b1-97c7-245ff04fdd33", agents.Agent{
		ID:           "d252397d-7bd9-41b1-97c7-245ff04fdd33",
		DisplayName:  "Owned User Agent",
		SharedSecret: "owned-user-agent-secret",
		Enabled:      true,
		UserIDs:      []string{created.ID},
	})
	deleteReferenced := userTargetMutationRequest(t, server, http.MethodDelete, "/v1/user/users/"+created.ID, cookie, session.CSRFToken, ``)
	if deleteReferenced.Code != http.StatusBadRequest {
		t.Fatalf("referenced delete status = %d body=%s", deleteReferenced.Code, deleteReferenced.Body.String())
	}
}

func TestUserAgentMutationsAdminCanUpdateAndDeleteForeignAgent(t *testing.T) {
	foreignID := "7937dd2d-0b8a-4880-8afa-0ea60fd65a43"
	server, paths := testTargetMutationServer(t, nil)
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
	foreignAgent := agents.Agent{
		ID:           foreignID,
		DisplayName:  "Foreign Agent",
		SharedSecret: "foreign-agent-secret",
		Enabled:      true,
		UserIDs:      []string{"a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5"},
	}
	writeJSONRecord(t, paths.AgentDir, foreignAgent.ID, foreignAgent)
	server.setAgentRegistry(agents.NewRegistry([]agents.Agent{testAgent, foreignAgent}))
	cookie, session := loginSession(t, server)

	updateForeign := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/agents/"+foreignID, cookie, session.CSRFToken, `{"display_name":"Updated Foreign Agent","enabled":false}`)
	if updateForeign.Code != http.StatusOK {
		t.Fatalf("foreign update status = %d body=%s", updateForeign.Code, updateForeign.Body.String())
	}
	updated, err := readStoredAgent(paths, foreignID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.DisplayName != "Updated Foreign Agent" || updated.Enabled || updated.SharedSecret != "foreign-agent-secret" {
		t.Fatalf("updated agent = %#v", updated)
	}
	if len(updated.UserIDs) != 1 || updated.UserIDs[0] != "a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5" {
		t.Fatalf("updated agent user ids = %#v", updated.UserIDs)
	}

	deleteForeign := userTargetMutationRequest(t, server, http.MethodDelete, "/v1/user/agents/"+foreignID, cookie, session.CSRFToken, ``)
	if deleteForeign.Code != http.StatusOK {
		t.Fatalf("foreign delete status = %d body=%s", deleteForeign.Code, deleteForeign.Body.String())
	}
	if _, err := readStoredAgent(paths, foreignID); !os.IsNotExist(err) {
		t.Fatalf("expected deleted agent file, err=%v", err)
	}
}

func TestUserInteractionsEndpointFiltersByOwnerAndAdminSeesAll(t *testing.T) {
	server, _ := testInteractionMutationServer(t)
	operator := users.User{
		ID:         "a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5",
		Username:   "operator",
		Name:       "Operator",
		Admin:      false,
		PasswdHash: testUser.PasswdHash,
	}
	operatorCookie, _ := addAuthenticatedTestUser(t, server, operator)
	writeTestNtfyInteraction(t, server.ManagementPaths.InteractionConfigDir, "admin.json", "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8", testUser.ID, "Admin ntfy", "")
	writeTestNtfyInteraction(t, server.ManagementPaths.InteractionConfigDir, "operator.json", "7937dd2d-0b8a-4880-8afa-0ea60fd65a43", operator.ID, "Operator ntfy", "")
	referencingAgent := testAgent
	referencingAgent.UserIDs = []string{operator.ID}
	referencingAgent.InteractionInstanceID = "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8"
	server.setAgentRegistry(agents.NewRegistry([]agents.Agent{referencingAgent}))

	operatorReq := httptest.NewRequest(http.MethodGet, "/v1/user/interactions", nil)
	operatorReq.AddCookie(operatorCookie)
	operatorRec := httptest.NewRecorder()
	server.Routes().ServeHTTP(operatorRec, operatorReq)
	if operatorRec.Code != http.StatusOK {
		t.Fatalf("operator status = %d body=%s", operatorRec.Code, operatorRec.Body.String())
	}
	var operatorResponse UserInteractionsResponse
	if err := json.Unmarshal(operatorRec.Body.Bytes(), &operatorResponse); err != nil {
		t.Fatal(err)
	}
	if len(operatorResponse.Interactions) != 2 {
		t.Fatalf("operator response = %#v", operatorResponse)
	}

	adminCookie := loginCookie(t, server)
	adminReq := httptest.NewRequest(http.MethodGet, "/v1/user/interactions", nil)
	adminReq.AddCookie(adminCookie)
	adminRec := httptest.NewRecorder()
	server.Routes().ServeHTTP(adminRec, adminReq)
	if adminRec.Code != http.StatusOK {
		t.Fatalf("admin status = %d body=%s", adminRec.Code, adminRec.Body.String())
	}
	var adminResponse UserInteractionsResponse
	if err := json.Unmarshal(adminRec.Body.Bytes(), &adminResponse); err != nil {
		t.Fatal(err)
	}
	if len(adminResponse.Interactions) != 2 {
		t.Fatalf("admin response = %#v", adminResponse)
	}
}

func TestUserInteractionMutationsCreateUpdateAndPreserveToken(t *testing.T) {
	server, paths := testInteractionMutationServer(t)
	operator := users.User{
		ID:         "a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5",
		Username:   "operator",
		Name:       "Operator",
		Admin:      false,
		PasswdHash: testUser.PasswdHash,
	}
	cookie, csrf := addAuthenticatedTestUser(t, server, operator)

	createRec := userTargetMutationRequest(t, server, http.MethodPost, "/v1/user/interactions", cookie, csrf, `{
  "type": "ntfy",
  "display_name": "Operator phone",
  "enabled": true,
  "base_url": "https://ntfy.example",
  "request_topic": "operator-requests",
  "response_topic": "operator-responses",
  "token": "secret-token",
  "poll_interval": "2s",
  "poll_timeout": "3s",
  "responded_by": "operator-phone"
}`)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created UserInteractionResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.CreatorUserID != operator.ID || !created.TokenConfigured || strings.Contains(createRec.Body.String(), "secret-token") {
		t.Fatalf("created response = %#v body=%s", created, createRec.Body.String())
	}

	updateRec := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/interactions/"+created.ID, cookie, csrf, `{
  "type": "ntfy",
  "display_name": "Renamed phone",
  "enabled": true,
  "base_url": "https://ntfy.example",
  "request_topic": "renamed-requests",
  "response_topic": "renamed-responses",
  "poll_interval": "4s",
  "poll_timeout": "5s",
  "responded_by": "operator-phone"
}`)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", updateRec.Code, updateRec.Body.String())
	}
	_, stored, err := interactioninstances.FindNtfy(paths.InteractionConfigDir, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Token != "secret-token" || stored.DisplayName != "Renamed phone" || stored.RequestTopic != "renamed-requests" {
		t.Fatalf("stored = %#v", stored)
	}
}

func TestUserInteractionMutationsRejectForeignAndReferencedDeletes(t *testing.T) {
	server, paths := testInteractionMutationServer(t)
	operator := users.User{
		ID:         "a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5",
		Username:   "operator",
		Name:       "Operator",
		Admin:      false,
		PasswdHash: testUser.PasswdHash,
	}
	operatorCookie, operatorCSRF := addAuthenticatedTestUser(t, server, operator)
	instanceID := "6a5b2d49-8c61-4f2e-9f1a-7f47ddbd71a8"
	writeTestNtfyInteraction(t, paths.InteractionConfigDir, "admin.json", instanceID, testUser.ID, "Admin ntfy", "")

	foreignUpdate := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/interactions/"+instanceID, operatorCookie, operatorCSRF, `{"type":"ntfy","display_name":"Nope"}`)
	if foreignUpdate.Code != http.StatusForbidden {
		t.Fatalf("foreign update status = %d body=%s", foreignUpdate.Code, foreignUpdate.Body.String())
	}

	referencingAgent := testAgent
	referencingAgent.UserIDs = []string{operator.ID}
	referencingAgent.InteractionInstanceID = instanceID
	server.setAgentRegistry(agents.NewRegistry([]agents.Agent{referencingAgent}))
	referencedUpdate := userTargetMutationRequest(t, server, http.MethodPut, "/v1/user/interactions/"+instanceID, operatorCookie, operatorCSRF, `{"type":"ntfy","display_name":"Shared ntfy"}`)
	if referencedUpdate.Code != http.StatusOK {
		t.Fatalf("referenced update status = %d body=%s", referencedUpdate.Code, referencedUpdate.Body.String())
	}
	adminCookie, adminSession := loginSession(t, server)
	deleteRec := userTargetMutationRequest(t, server, http.MethodDelete, "/v1/user/interactions/"+instanceID, adminCookie, adminSession.CSRFToken, "")
	if deleteRec.Code != http.StatusBadRequest || !strings.Contains(deleteRec.Body.String(), "referenced by agent") {
		t.Fatalf("delete status = %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
}

func TestUserCommandHistoryEndpointFiltersVisibleTasksAndRedactsSecrets(t *testing.T) {
	server := testLoginServer(t, false)
	currentUser, found := server.userRegistry().Find(testUser.ID)
	if !found {
		t.Fatal("missing current user")
	}
	currentUser.Admin = false
	server.setUserRegistry(users.NewRegistry([]users.User{currentUser}))
	ownedAgent := agents.Agent{
		ID:           testAgent.ID,
		DisplayName:  "Owned Agent",
		SharedSecret: "owned-agent-secret",
		Enabled:      true,
		UserIDs:      []string{testUser.ID},
	}
	otherAgent := agents.Agent{
		ID:           "7937dd2d-0b8a-4880-8afa-0ea60fd65a43",
		DisplayName:  "Other Agent",
		SharedSecret: "other-agent-secret",
		Enabled:      true,
		UserIDs:      []string{"a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5"},
	}
	server.Agents = agents.NewRegistry([]agents.Agent{ownedAgent, otherAgent})
	ownedTarget := targets.Target{
		ID:        "8dc60324-b3bb-4d19-9aa6-0e149e4938b8",
		Hostname:  "owned.example",
		IP:        "10.0.0.1",
		Username:  "owned-user",
		PolicyIDs: []string{"6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"},
		AgentIDs:  []string{ownedAgent.ID, otherAgent.ID},
		UserIDs:   []string{testUser.ID},
		Auth:      targets.PasswordAuth("owned-target-password"),
	}
	otherTarget := targets.Target{
		ID:        "a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5",
		Hostname:  "other.example",
		IP:        "10.0.0.2",
		Username:  "other-user",
		PolicyIDs: []string{"6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"},
		AgentIDs:  []string{otherAgent.ID},
		UserIDs:   []string{"7937dd2d-0b8a-4880-8afa-0ea60fd65a43"},
		Auth:      targets.PasswordAuth("other-target-password"),
	}
	server.Targets = targets.NewRegistry([]targets.Target{ownedTarget, otherTarget})

	visibleByAgent, err := server.Tasks.Submit(context.Background(), tasks.CommandContext{
		AgentID:   ownedAgent.ID,
		Target:    &tasks.Target{Hostname: otherTarget.Hostname, IP: otherTarget.IP},
		Command:   "ssh {user}@other.example hostname",
		Reasoning: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	visibleByTarget, err := server.Tasks.Submit(context.Background(), tasks.CommandContext{
		AgentID:   otherAgent.ID,
		Target:    &tasks.Target{Hostname: ownedTarget.Hostname, IP: ownedTarget.IP},
		Command:   "ssh {user}@owned.example uptime",
		Reasoning: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := server.Tasks.Submit(context.Background(), tasks.CommandContext{
		AgentID:   otherAgent.ID,
		Target:    &tasks.Target{Hostname: otherTarget.Hostname, IP: otherTarget.IP},
		Command:   "ssh {user}@other.example whoami",
		Reasoning: "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	cookie := loginCookie(t, server)
	req := httptest.NewRequest(http.MethodGet, "/v1/user/command-history?limit=10", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response UserCommandHistoryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Commands) != 2 {
		t.Fatalf("response = %#v", response)
	}
	if !containsCommandTask(response.Commands, visibleByAgent.TaskID) || !containsCommandTask(response.Commands, visibleByTarget.TaskID) {
		t.Fatalf("visible tasks missing from response = %#v", response)
	}
	if containsCommandTask(response.Commands, hidden.TaskID) {
		t.Fatalf("hidden task leaked in response = %#v", response)
	}
	body := rec.Body.String()
	for _, leaked := range []string{"owned-agent-secret", "other-agent-secret", "owned-target-password", "other-target-password", "owned-user", "other-user"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("history response leaked %q: %s", leaked, body)
		}
	}
}

func TestUserCommandHistoryEndpointAdminSeesAllTasksAndRedactsSecrets(t *testing.T) {
	server := testLoginServer(t, false)
	ownedAgent := agents.Agent{
		ID:           testAgent.ID,
		DisplayName:  "Owned Agent",
		SharedSecret: "owned-agent-secret",
		Enabled:      true,
		UserIDs:      []string{testUser.ID},
	}
	otherAgent := agents.Agent{
		ID:           "7937dd2d-0b8a-4880-8afa-0ea60fd65a43",
		DisplayName:  "Other Agent",
		SharedSecret: "other-agent-secret",
		Enabled:      true,
		UserIDs:      []string{"a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5"},
	}
	server.Agents = agents.NewRegistry([]agents.Agent{ownedAgent, otherAgent})
	ownedTarget := targets.Target{
		ID:        "8dc60324-b3bb-4d19-9aa6-0e149e4938b8",
		Hostname:  "owned.example",
		IP:        "10.0.0.1",
		Username:  "owned-user",
		PolicyIDs: []string{"6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"},
		AgentIDs:  []string{ownedAgent.ID, otherAgent.ID},
		UserIDs:   []string{testUser.ID},
		Auth:      targets.PasswordAuth("owned-target-password"),
	}
	otherTarget := targets.Target{
		ID:        "a49e7f6c-aa6f-40cd-ac95-e2c876db5bf5",
		Hostname:  "other.example",
		IP:        "10.0.0.2",
		Username:  "other-user",
		PolicyIDs: []string{"6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c"},
		AgentIDs:  []string{otherAgent.ID},
		UserIDs:   []string{"7937dd2d-0b8a-4880-8afa-0ea60fd65a43"},
		Auth:      targets.PasswordAuth("other-target-password"),
	}
	server.Targets = targets.NewRegistry([]targets.Target{ownedTarget, otherTarget})

	visibleByAgent, err := server.Tasks.Submit(context.Background(), tasks.CommandContext{
		AgentID:   ownedAgent.ID,
		Target:    &tasks.Target{Hostname: otherTarget.Hostname, IP: otherTarget.IP},
		Command:   "ssh {user}@other.example hostname",
		Reasoning: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	visibleByTarget, err := server.Tasks.Submit(context.Background(), tasks.CommandContext{
		AgentID:   otherAgent.ID,
		Target:    &tasks.Target{Hostname: ownedTarget.Hostname, IP: ownedTarget.IP},
		Command:   "ssh {user}@owned.example uptime",
		Reasoning: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := server.Tasks.Submit(context.Background(), tasks.CommandContext{
		AgentID:   otherAgent.ID,
		Target:    &tasks.Target{Hostname: otherTarget.Hostname, IP: otherTarget.IP},
		Command:   "ssh {user}@other.example whoami",
		Reasoning: "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	cookie := loginCookie(t, server)
	req := httptest.NewRequest(http.MethodGet, "/v1/user/command-history?limit=10", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response UserCommandHistoryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Commands) != 3 {
		t.Fatalf("response = %#v", response)
	}
	for _, taskID := range []string{visibleByAgent.TaskID, visibleByTarget.TaskID, hidden.TaskID} {
		if !containsCommandTask(response.Commands, taskID) {
			t.Fatalf("task %s missing from response = %#v", taskID, response)
		}
	}
	body := rec.Body.String()
	for _, leaked := range []string{"owned-agent-secret", "other-agent-secret", "owned-target-password", "other-target-password", "owned-user", "other-user"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("history response leaked %q: %s", leaked, body)
		}
	}
}

func TestCommandHistoryLimitCapsAtMaximum(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/user/command-history?limit=9999", nil)
	limit, err := commandHistoryLimit(req)
	if err != nil {
		t.Fatal(err)
	}
	if limit != maxCommandHistoryLimit {
		t.Fatalf("limit = %d", limit)
	}
	if maxCommandHistoryLimit != 500 {
		t.Fatalf("maxCommandHistoryLimit = %d", maxCommandHistoryLimit)
	}
}

func TestUserResourceEndpointsRequireSessionAndGet(t *testing.T) {
	server := testLoginServer(t, false)
	for _, path := range []string{"/v1/user/agents", "/v1/user/targets", "/v1/user/command-history"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		server.Routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s unauthenticated status = %d body=%s", path, rec.Code, rec.Body.String())
		}
	}

	cookie := loginCookie(t, server)
	agentPost := httptest.NewRequest(http.MethodPost, "/v1/user/agents", nil)
	agentPost.AddCookie(cookie)
	agentPostRec := httptest.NewRecorder()
	server.Routes().ServeHTTP(agentPostRec, agentPost)
	if agentPostRec.Code != http.StatusForbidden {
		t.Fatalf("/v1/user/agents post without csrf status = %d body=%s", agentPostRec.Code, agentPostRec.Body.String())
	}
	for _, path := range []string{"/v1/user/command-history"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		server.Routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s post status = %d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

func containsCommandTask(items []UserCommandHistoryItemResponse, taskID string) bool {
	for _, item := range items {
		if item.TaskID == taskID {
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

func loginCookie(t *testing.T, server *Server) *http.Cookie {
	t.Helper()
	cookie, _ := loginSession(t, server)
	return cookie
}

func loginSession(t *testing.T, server *Server) (*http.Cookie, UserSessionResponse) {
	t.Helper()
	loginRec := loginRequest(t, server, `{"username":"antoine","password":"correct-password"}`)
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
			if path != "/v1/user/targets/test-credentials" && (method == http.MethodPost || method == http.MethodPut) {
				encoded, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				var request UserTargetMutationRequest
				if err := json.Unmarshal(encoded, &request); err != nil {
					t.Fatal(err)
				}
				targetID := ""
				if method == http.MethodPut {
					targetID = strings.TrimPrefix(path, "/v1/user/targets/")
				}
				token, err := server.targetCredentialProofs().issue(testUser.ID, targetID, credentialRequestDigest(request))
				if err != nil {
					t.Fatal(err)
				}
				payload["credential_test_token"] = token
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
	if path != "/v1/user/targets/test-credentials" && (method == http.MethodPost || method == http.MethodPut) {
		request := UserTargetMutationRequest{
			Transport: firstFormValue(fields, "transport"), Hostname: firstFormValue(fields, "hostname"), IP: firstFormValue(fields, "ip"),
			Username: firstFormValue(fields, "username"), AuthType: firstFormValue(fields, "auth_type"), Password: firstFormValue(fields, "password"),
			PrivateKeyUpload: privateKey,
		}
		targetID := ""
		if method == http.MethodPut {
			targetID = strings.TrimPrefix(path, "/v1/user/targets/")
		}
		token, err := server.targetCredentialProofs().issue(testUser.ID, targetID, credentialRequestDigest(request))
		if err != nil {
			t.Fatal(err)
		}
		fields["credential_test_token"] = []string{token}
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
		"id":          "6e05e4a3-2e06-4d8e-92dd-7d5a4b0e0e4c",
		"name":        "Test baseline policy",
		"description": "Test baseline policy",
		"priority":    100,
		"enabled":     true,
		"rules": []map[string]any{
			{
				"id":     "allow-all",
				"effect": "allow",
				"always": true,
				"reason": "test policy",
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
