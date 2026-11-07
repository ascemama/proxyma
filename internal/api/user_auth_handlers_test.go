package api

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"proxyma/internal/users"
	"strings"
	"testing"
	"time"
)

func TestLoginCreatesSecureUserSessionCookie(t *testing.T) {
	server := testLoginServer(t, false)
	rec := loginRequest(t, server, `{"username":"demo-admin","password":"correct-password"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response UserSessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.User.ID != testUser.ID || response.User.Username != "demo-admin" || response.User.Theme != users.ThemeLight || response.CSRFToken == "" {
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
	rec := loginRequest(t, server, `{"username":"demo-admin","password":"correct-password"}`)
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
		{name: "missing password", body: `{"username":"demo-admin"}`, status: http.StatusBadRequest},
		{name: "unknown field", body: `{"username":"demo-admin","password":"correct-password","extra":true}`, status: http.StatusBadRequest},
		{name: "wrong password", body: `{"username":"demo-admin","password":"wrong-password"}`, status: http.StatusUnauthorized, code: "invalid_login"},
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
	loginRec := loginRequest(t, server, `{"username":"demo-admin","password":"correct-password"}`)
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

	oldLogin := loginRequest(t, server, `{"username":"demo-admin","password":"correct-password"}`)
	if oldLogin.Code != http.StatusUnauthorized {
		t.Fatalf("old login status = %d body=%s", oldLogin.Code, oldLogin.Body.String())
	}
	newLogin := loginRequest(t, server, `{"username":"demo-admin","password":"new-password"}`)
	if newLogin.Code != http.StatusOK {
		t.Fatalf("new login status = %d body=%s", newLogin.Code, newLogin.Body.String())
	}
	logText := logs.String()
	for _, expected := range []string{"user password changed", testUser.ID, `username="demo-admin"`, "revoked_sessions=1"} {
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
	legacyPath := filepath.Join(paths.UserDir, "demo-admin.json")
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

func TestUserResourceEndpointsRequireSessionAndGet(t *testing.T) {
	server := testLoginServer(t, false)
	for _, path := range []string{"/v1/user/agents", "/v1/user/targets", "/v1/user/history"} {
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
	for _, path := range []string{"/v1/user/history"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		server.Routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s post status = %d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}
