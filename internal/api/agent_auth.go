package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"proxyma/internal/agents"
)

func (s *Server) authenticateAgent(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenString, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok {
			s.debugAgentAuthFailure(r, "auth_required", "missing bearer token")
			writeAgentAuthError(w, "auth_required", "missing bearer token", false, "")
			return
		}
		agent, err := agents.VerifyToken(s.agentRegistry(), tokenString, time.Now())
		if err != nil {
			if errors.Is(err, agents.ErrTokenExpired) {
				s.debugAgentAuthFailure(r, "token_expired", "token expired")
				writeAgentAuthError(w, "token_expired", "token expired", true, "regenerate Proxyma JWT and retry")
				return
			}
			s.debugAgentAuthFailure(r, "invalid_token", "invalid bearer token")
			writeAgentAuthError(w, "invalid_token", "invalid bearer token", false, "")
			return
		}
		next.ServeHTTP(w, r.WithContext(contextWithAgent(r.Context(), agent)))
	})
}

func (s *Server) debugAgentAuthFailure(r *http.Request, code string, message string) {
	s.debugf("debug agent auth failed method=%s path=%s remote=%s code=%s error=%q",
		r.Method, r.URL.RequestURI(), r.RemoteAddr, code, message)
}

func bearerToken(header string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	return token, token != ""
}

func writeAgentAuthError(w http.ResponseWriter, code string, message string, retryable bool, hint string) {
	w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer error="%s"`, code))
	writeJSON(w, http.StatusUnauthorized, ErrorResponse{
		Error:     message,
		Code:      code,
		Retryable: retryable,
		Hint:      hint,
	})
}
