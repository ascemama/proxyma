package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"proxyma/internal/targets"
)

func TestResolveTarget(t *testing.T) {
	registry := targets.NewRegistry([]targets.Target{
		{Transport: targets.TransportSSH, OS: targets.OSLinux, Hostname: "linux.example", IP: "10.0.0.10", Username: "linux", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("p")},
		{Transport: targets.TransportSSH, OS: targets.OSWindows, Hostname: "win-ssh.example", IP: "10.0.0.11", Username: "windows", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("p")},
		{Transport: targets.TransportWinRM, OS: targets.OSWindows, Hostname: "winrm.example", IP: "10.0.0.12", Username: "Administrator", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("p"), WinRM: &targets.WinRM{Scheme: "https", Port: 5986}},
	})
	server := testServerWithRegistry(t, registry)

	cases := []struct {
		name        string
		path        string
		wantStatus  int
		wantPrefix  string
		wantPayload string
		wantOS      string
	}{
		{name: "ssh linux", path: "/v1/agent/targets/resolve?ip=10.0.0.10", wantStatus: http.StatusOK, wantPrefix: "ssh {user}@10.0.0.10 ", wantPayload: "posix_shell", wantOS: targets.OSLinux},
		{name: "ssh windows", path: "/v1/agent/targets/resolve?hostname=win-ssh.example", wantStatus: http.StatusOK, wantPrefix: "ssh {user}@win-ssh.example ", wantPayload: "windows_shell", wantOS: targets.OSWindows},
		{name: "winrm windows", path: "/v1/agent/targets/resolve?ip=10.0.0.12", wantStatus: http.StatusOK, wantPrefix: "winrm {user}@10.0.0.12 ", wantPayload: "powershell", wantOS: targets.OSWindows},
		{name: "matching hostname and ip", path: "/v1/agent/targets/resolve?hostname=linux.example&ip=10.0.0.10", wantStatus: http.StatusOK, wantPrefix: "ssh {user}@10.0.0.10 ", wantPayload: "posix_shell", wantOS: targets.OSLinux},
		{name: "missing target", path: "/v1/agent/targets/resolve", wantStatus: http.StatusBadRequest},
		{name: "unknown target", path: "/v1/agent/targets/resolve?ip=192.0.2.10", wantStatus: http.StatusNotFound},
		{name: "hostname ip conflict", path: "/v1/agent/targets/resolve?hostname=linux.example&ip=10.0.0.11", wantStatus: http.StatusBadRequest},
		{name: "unsupported parameter", path: "/v1/agent/targets/resolve?target=linux.example", wantStatus: http.StatusBadRequest},
		{name: "repeated parameter", path: "/v1/agent/targets/resolve?ip=10.0.0.10&ip=10.0.0.10", wantStatus: http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := newAuthenticatedRequest(t, http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			server.Routes().ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantStatus != http.StatusOK {
				return
			}
			var response ResolveTargetResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Target.OS != tc.wantOS || response.CanonicalCommand.Prefix != tc.wantPrefix || response.CanonicalCommand.PayloadKind != tc.wantPayload {
				t.Fatalf("response = %#v", response)
			}
			if strings.Contains(rec.Body.String(), "Administrator") || strings.Contains(rec.Body.String(), `"auth"`) || strings.Contains(rec.Body.String(), "5986") {
				t.Fatalf("response leaked unsafe target metadata: %s", rec.Body.String())
			}
		})
	}
}

func TestResolveTargetHidesUnauthorizedTargetsAndRejectsDuplicates(t *testing.T) {
	t.Run("unauthorized target", func(t *testing.T) {
		server := testServerWithRegistry(t, targets.NewRegistry([]targets.Target{
			{Transport: targets.TransportSSH, OS: targets.OSLinux, Hostname: "linux.example", IP: "10.0.0.10", Username: "linux", AgentIDs: []string{"7937dd2d-0b8a-4880-8afa-0ea60fd65a43"}, Auth: targets.PasswordAuth("p")},
		}))
		req := newAuthenticatedRequest(t, http.MethodGet, "/v1/agent/targets/resolve?ip=10.0.0.10", nil)
		rec := httptest.NewRecorder()
		server.Routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
		}
		var unauthorized ErrorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &unauthorized); err != nil {
			t.Fatal(err)
		}

		unknownReq := newAuthenticatedRequest(t, http.MethodGet, "/v1/agent/targets/resolve?ip=192.0.2.55", nil)
		unknownRec := httptest.NewRecorder()
		server.Routes().ServeHTTP(unknownRec, unknownReq)
		if unknownRec.Code != http.StatusNotFound {
			t.Fatalf("unknown status = %d, want %d; body=%s", unknownRec.Code, http.StatusNotFound, unknownRec.Body.String())
		}
		var unknown ErrorResponse
		if err := json.Unmarshal(unknownRec.Body.Bytes(), &unknown); err != nil {
			t.Fatal(err)
		}
		if unauthorized != unknown {
			t.Fatalf("unauthorized response differs from unknown: unauthorized=%#v unknown=%#v", unauthorized, unknown)
		}
	})

	t.Run("duplicate authorized target", func(t *testing.T) {
		server := testServerWithRegistry(t, targets.NewRegistry([]targets.Target{
			{Transport: targets.TransportSSH, OS: targets.OSLinux, Hostname: "dup.example", Username: "linux", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("p")},
			{Transport: targets.TransportSSH, OS: targets.OSWindows, IP: "dup.example", Username: "windows", AgentIDs: []string{testAgent.ID}, Auth: targets.PasswordAuth("p")},
		}))
		req := newAuthenticatedRequest(t, http.MethodGet, "/v1/agent/targets/resolve?hostname=dup.example", nil)
		rec := httptest.NewRecorder()
		server.Routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	})
}
