package agents

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const MaxTokenLifetime = 10 * time.Minute

var (
	ErrTokenExpired = errors.New("token expired")
	ErrInvalidToken = errors.New("invalid token")
)

// Claims are the Proxyma JWT claims.
type Claims struct {
	jwt.RegisteredClaims
}

// IssueToken creates a signed Proxyma JWT for an enabled agent.
func IssueToken(agent Agent, now time.Time) (string, error) {
	if !agent.Enabled {
		return "", fmt.Errorf("agent %q is disabled", agent.ID)
	}
	now = now.UTC()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   agent.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(MaxTokenLifetime)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["kid"] = agent.ID
	return token.SignedString([]byte(agent.AgentJWTSigningKey))
}

// VerifyToken validates a signed Proxyma JWT and returns the authenticated agent.
func VerifyToken(registry Registry, tokenString string, now time.Time) (Agent, error) {
	var selected Agent
	claims := &Claims{}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithTimeFunc(func() time.Time { return now.UTC() }),
	)
	token, err := parser.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, ErrInvalidToken
		}
		kid, ok := token.Header["kid"].(string)
		if !ok || normalizeID(kid) == "" {
			return nil, ErrInvalidToken
		}
		agent, found := registry.Find(kid)
		if !found || !agent.Enabled {
			return nil, ErrInvalidToken
		}
		selected = agent
		return []byte(agent.AgentJWTSigningKey), nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return Agent{}, ErrTokenExpired
		}
		return Agent{}, ErrInvalidToken
	}
	if token == nil || !token.Valid || selected.ID == "" {
		return Agent{}, ErrInvalidToken
	}
	if claims.Subject != selected.ID {
		return Agent{}, ErrInvalidToken
	}
	if claims.IssuedAt == nil || claims.ExpiresAt == nil {
		return Agent{}, ErrInvalidToken
	}
	if claims.ExpiresAt.Time.Sub(claims.IssuedAt.Time) > MaxTokenLifetime {
		return Agent{}, ErrInvalidToken
	}
	return selected, nil
}
