package api

import (
	"net/http"
	"time"
)

// Routes returns the Proxyma HTTP router.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/user/login", s.handleLogin)
	mux.HandleFunc("/v1/user/activation", s.handleActivation)
	mux.Handle("/v1/user/session", s.authenticateUserSession(http.HandlerFunc(s.handleSession)))
	mux.Handle("/v1/user/logout", s.authenticateUserSession(s.verifyUserSessionCSRF(http.HandlerFunc(s.handleLogout))))
	mux.Handle("/v1/user/change-password", s.authenticateUserSession(s.verifyUserSessionCSRF(http.HandlerFunc(s.handleChangePassword))))
	mux.Handle("/v1/user/settings", s.authenticateUserSession(s.verifyUserSessionCSRF(http.HandlerFunc(s.handleUserSettings))))
	mux.Handle("/v1/user/users", s.authenticateAdminSession(s.verifyUserSessionCSRF(http.HandlerFunc(s.handleUsers))))
	mux.Handle("/v1/user/users/", s.authenticateAdminSession(s.verifyUserSessionCSRF(http.HandlerFunc(s.handleUser))))
	mux.Handle("/v1/user/activation-policy", s.authenticateAdminSession(http.HandlerFunc(s.handleActivationPolicy)))
	mux.Handle("/v1/user/agents", s.authenticateUserSession(s.verifyUserSessionCSRF(http.HandlerFunc(s.handleUserAgents))))
	mux.Handle("/v1/user/agents/", s.authenticateUserSession(s.verifyUserSessionCSRF(http.HandlerFunc(s.handleUserAgent))))
	mux.Handle("/v1/user/targets/test-credentials", s.authenticateUserSession(s.verifyUserSessionCSRF(http.HandlerFunc(s.handleTestUserTargetCredentials))))
	mux.Handle("/v1/user/targets", s.authenticateUserSession(s.verifyUserSessionCSRF(http.HandlerFunc(s.handleUserTargets))))
	mux.Handle("/v1/user/targets/", s.authenticateUserSession(s.verifyUserSessionCSRF(http.HandlerFunc(s.handleUserTarget))))
	mux.Handle("/v1/user/interactions", s.authenticateUserSession(s.verifyUserSessionCSRF(http.HandlerFunc(s.handleUserInteractions))))
	mux.Handle("/v1/user/interactions/", s.authenticateUserSession(s.verifyUserSessionCSRF(http.HandlerFunc(s.handleUserInteraction))))
	mux.Handle("/v1/user/interaction-history", s.authenticateUserSession(http.HandlerFunc(s.handleUserInteractionHistory)))
	mux.Handle("/v1/user/policies", s.authenticateUserSession(s.verifyUserSessionCSRF(http.HandlerFunc(s.handleUserPolicies))))
	mux.Handle("/v1/user/policies/", s.authenticateUserSession(s.verifyUserSessionCSRF(http.HandlerFunc(s.handleUserPolicy))))
	mux.Handle("/v1/user/command-history", s.authenticateUserSession(http.HandlerFunc(s.handleUserCommandHistory)))
	mux.Handle("/v1/agent/commands", s.authenticateAgent(http.HandlerFunc(s.handleCommands)))
	mux.Handle("/v1/agent/targets/resolve", s.authenticateAgent(http.HandlerFunc(s.handleResolveTarget)))
	mux.Handle("/v1/agent/interactions/", s.authenticateAgent(http.HandlerFunc(s.handleAgentInteractions)))
	mux.Handle("/v1/agent/tasks/", s.authenticateAgent(http.HandlerFunc(s.handleTaskStatus)))
	mux.Handle("/v1/health", s.authenticateAgent(http.HandlerFunc(s.handleHealth)))
	handler := http.Handler(mux)
	if s.Debug {
		return s.debugRequests(handler)
	}
	return handler
}

func (s *Server) debugRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		s.debugf("debug http request method=%s path=%s remote=%s status=%d duration=%s bytes=%d",
			r.Method, r.URL.RequestURI(), r.RemoteAddr, recorder.status, time.Since(start), r.ContentLength)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
