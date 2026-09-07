package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"proxyma/internal/targets"
)

const credentialProofTTL = 10 * time.Minute

type credentialProof struct {
	UserID    string
	TargetID  string
	Digest    string
	ExpiresAt time.Time
}

type credentialProofStore struct {
	mu      sync.Mutex
	records map[string]credentialProof
	now     func() time.Time
}

func newCredentialProofStore() *credentialProofStore {
	return &credentialProofStore{records: map[string]credentialProof{}, now: time.Now}
}

func (s *credentialProofStore) issue(userID, targetID, digest string) (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate credential test token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(buffer)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.purgeExpired(now)
	s.records[token] = credentialProof{UserID: userID, TargetID: targetID, Digest: digest, ExpiresAt: now.Add(credentialProofTTL)}
	return token, nil
}

func (s *credentialProofStore) consume(token, userID, targetID, digest string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.purgeExpired(now)
	record, ok := s.records[token]
	if !ok || record.UserID != userID || record.TargetID != targetID || record.Digest != digest {
		return false
	}
	delete(s.records, token)
	return true
}

func (s *credentialProofStore) purgeExpired(now time.Time) {
	for token, record := range s.records {
		if !record.ExpiresAt.After(now) {
			delete(s.records, token)
		}
	}
}

type credentialDigestInput struct {
	Transport        string           `json:"transport"`
	Hostname         string           `json:"hostname"`
	IP               string           `json:"ip"`
	Username         string           `json:"username"`
	AuthType         string           `json:"auth_type"`
	PasswordDigest   string           `json:"password_digest,omitempty"`
	PrivateKeyDigest string           `json:"private_key_digest,omitempty"`
	WinRM            *UserTargetWinRM `json:"winrm,omitempty"`
}

func credentialRequestDigest(request UserTargetMutationRequest) string {
	input := credentialDigestInput{
		Transport: strings.TrimSpace(request.Transport),
		Hostname:  strings.TrimSpace(request.Hostname),
		IP:        strings.TrimSpace(request.IP),
		Username:  strings.TrimSpace(request.Username),
		AuthType:  strings.TrimSpace(request.AuthType),
	}
	if input.AuthType == "" {
		input.AuthType = targets.AuthTypePassword
	}
	if request.Password != "" {
		input.PasswordDigest = sha256Hex([]byte(request.Password))
	}
	if len(request.PrivateKeyUpload) > 0 {
		input.PrivateKeyDigest = sha256Hex(request.PrivateKeyUpload)
	}
	if request.WinRM != nil {
		winrm := *request.WinRM
		winrm.Scheme = strings.ToLower(strings.TrimSpace(winrm.Scheme))
		input.WinRM = &winrm
	}
	encoded, _ := json.Marshal(input)
	return sha256Hex(encoded)
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func targetCredentialChanged(existing *targets.Target, request UserTargetMutationRequest) bool {
	if existing == nil {
		return true
	}
	transport := strings.TrimSpace(request.Transport)
	if transport == "" {
		transport = existing.Transport
	}
	if transport != existing.Transport || strings.TrimSpace(request.Hostname) != existing.Hostname || strings.TrimSpace(request.IP) != existing.IP {
		return true
	}
	if username := strings.TrimSpace(request.Username); username != "" && username != existing.Username {
		return true
	}
	existingAuthType := ""
	if existing.Auth != nil {
		existingAuthType = existing.Auth.Type
	}
	if authType := strings.TrimSpace(request.AuthType); authType != "" && authType != existingAuthType {
		return true
	}
	if request.Password != "" || len(request.PrivateKeyUpload) > 0 {
		return true
	}
	if request.WinRM != nil {
		if existing.WinRM == nil || strings.ToLower(strings.TrimSpace(request.WinRM.Scheme)) != existing.WinRM.Scheme || request.WinRM.Port != existing.WinRM.Port || request.WinRM.TLSInsecure != existing.WinRM.TLSInsecure {
			return true
		}
	}
	return false
}

func credentialTestRequiredError() error {
	return requestError{status: 400, payload: ErrorResponse{Error: "a successful credential test is required", Code: "credential_test_required"}}
}
