package agents

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var testNow = time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)

func TestIssueAndVerifyToken(t *testing.T) {
	agent := Agent{ID: "df9ec8bb-1a22-4181-a7c8-126ad553221d", SharedSecret: "secret", Enabled: true}
	token, err := IssueToken(agent, testNow)
	if err != nil {
		t.Fatal(err)
	}

	authenticated, err := VerifyToken(NewRegistry([]Agent{agent}), token, testNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if authenticated.ID != agent.ID {
		t.Fatalf("agent = %#v", authenticated)
	}
}

func TestIssueTokenRejectsDisabledAgent(t *testing.T) {
	_, err := IssueToken(Agent{ID: "df9ec8bb-1a22-4181-a7c8-126ad553221d", SharedSecret: "secret"}, testNow)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestVerifyTokenRejectsBadSignature(t *testing.T) {
	agent := Agent{ID: "df9ec8bb-1a22-4181-a7c8-126ad553221d", SharedSecret: "secret", Enabled: true}
	token := signTestToken(t, agent.ID, agent.ID, "wrong-secret", jwt.SigningMethodHS256, testNow, testNow.Add(MaxTokenLifetime))
	_, err := VerifyToken(NewRegistry([]Agent{agent}), token, testNow)
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("err = %v", err)
	}
}

func TestVerifyTokenRejectsExpiredToken(t *testing.T) {
	agent := Agent{ID: "df9ec8bb-1a22-4181-a7c8-126ad553221d", SharedSecret: "secret", Enabled: true}
	token := signTestToken(t, agent.ID, agent.ID, agent.SharedSecret, jwt.SigningMethodHS256, testNow.Add(-MaxTokenLifetime), testNow.Add(-time.Second))
	_, err := VerifyToken(NewRegistry([]Agent{agent}), token, testNow)
	if !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("err = %v", err)
	}
}

func TestVerifyTokenRejectsDisabledAgent(t *testing.T) {
	agent := Agent{ID: "df9ec8bb-1a22-4181-a7c8-126ad553221d", SharedSecret: "secret"}
	token := signTestToken(t, agent.ID, agent.ID, agent.SharedSecret, jwt.SigningMethodHS256, testNow, testNow.Add(MaxTokenLifetime))
	_, err := VerifyToken(NewRegistry([]Agent{agent}), token, testNow)
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("err = %v", err)
	}
}

func TestVerifyTokenRejectsMissingExpiration(t *testing.T) {
	agent := Agent{ID: "df9ec8bb-1a22-4181-a7c8-126ad553221d", SharedSecret: "secret", Enabled: true}
	claims := Claims{RegisteredClaims: jwt.RegisteredClaims{
		Subject:  agent.ID,
		IssuedAt: jwt.NewNumericDate(testNow),
	}}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["kid"] = agent.ID
	signed, err := token.SignedString([]byte(agent.SharedSecret))
	if err != nil {
		t.Fatal(err)
	}

	_, err = VerifyToken(NewRegistry([]Agent{agent}), signed, testNow)
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("err = %v", err)
	}
}

func TestVerifyTokenRejectsLongLifetime(t *testing.T) {
	agent := Agent{ID: "df9ec8bb-1a22-4181-a7c8-126ad553221d", SharedSecret: "secret", Enabled: true}
	token := signTestToken(t, agent.ID, agent.ID, agent.SharedSecret, jwt.SigningMethodHS256, testNow, testNow.Add(MaxTokenLifetime+time.Second))
	_, err := VerifyToken(NewRegistry([]Agent{agent}), token, testNow)
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("err = %v", err)
	}
}

func TestVerifyTokenRejectsWrongAlgorithm(t *testing.T) {
	agent := Agent{ID: "df9ec8bb-1a22-4181-a7c8-126ad553221d", SharedSecret: "secret", Enabled: true}
	token := signTestToken(t, agent.ID, agent.ID, agent.SharedSecret, jwt.SigningMethodHS384, testNow, testNow.Add(MaxTokenLifetime))
	_, err := VerifyToken(NewRegistry([]Agent{agent}), token, testNow)
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("err = %v", err)
	}
}

func TestVerifyTokenRejectsSubjectKeyIDMismatch(t *testing.T) {
	agent := Agent{ID: "df9ec8bb-1a22-4181-a7c8-126ad553221d", SharedSecret: "secret", Enabled: true}
	token := signTestToken(t, agent.ID, "7937dd2d-0b8a-4880-8afa-0ea60fd65a43", agent.SharedSecret, jwt.SigningMethodHS256, testNow, testNow.Add(MaxTokenLifetime))
	_, err := VerifyToken(NewRegistry([]Agent{agent}), token, testNow)
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("err = %v", err)
	}
}

func TestVerifyTokenRejectsCrossSignedAgent(t *testing.T) {
	codex := Agent{ID: "df9ec8bb-1a22-4181-a7c8-126ad553221d", SharedSecret: "codex-secret", Enabled: true}
	automation := Agent{ID: "7937dd2d-0b8a-4880-8afa-0ea60fd65a43", SharedSecret: "automation-secret", Enabled: true}
	token := signTestToken(t, automation.ID, automation.ID, codex.SharedSecret, jwt.SigningMethodHS256, testNow, testNow.Add(MaxTokenLifetime))
	_, err := VerifyToken(NewRegistry([]Agent{codex, automation}), token, testNow)
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("err = %v", err)
	}
}

func signTestToken(t *testing.T, kid, subject, secret string, method jwt.SigningMethod, issuedAt, expiresAt time.Time) string {
	t.Helper()
	claims := Claims{RegisteredClaims: jwt.RegisteredClaims{
		Subject:   subject,
		IssuedAt:  jwt.NewNumericDate(issuedAt),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
	}}
	token := jwt.NewWithClaims(method, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return signed
}
