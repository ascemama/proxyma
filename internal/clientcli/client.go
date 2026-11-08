package clientcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const idempotencyCapability = "agent_command_idempotency_v1"

type Client struct {
	cfg        Config
	http       *http.Client
	mu         sync.Mutex
	token      string
	tokenUntil time.Time
}

type healthResponse struct {
	Status       string   `json:"status"`
	Capabilities []string `json:"capabilities"`
}

type commandRequest struct {
	ClientOperationID string         `json:"client_operation_id,omitempty"`
	Command           string         `json:"command"`
	Reasoning         string         `json:"reasoning"`
	Target            Target         `json:"target"`
	RequestedBy       string         `json:"requested_by,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

type commandResponse struct {
	TaskID            string `json:"task_id"`
	Status            string `json:"status"`
	ClientOperationID string `json:"client_operation_id,omitempty"`
	Replayed          bool   `json:"replayed,omitempty"`
}

type taskResponse struct {
	TaskID            string  `json:"task_id"`
	ClientOperationID string  `json:"client_operation_id,omitempty"`
	Status            string  `json:"status"`
	Result            *Result `json:"result,omitempty"`
}

type apiErrorResponse struct {
	Error     string `json:"error"`
	Code      string `json:"code,omitempty"`
	Retryable bool   `json:"retryable,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

func NewClient(cfg Config) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: cfg.HTTPTimeout()}}
}

func (c *Client) Health(ctx context.Context) error {
	var response healthResponse
	if err := c.request(ctx, http.MethodGet, "/v1/health", nil, &response); err != nil {
		return err
	}
	if response.Status != "ok" {
		return proxymaInvalidResponse("Proxyma health response did not report status ok")
	}
	for _, capability := range response.Capabilities {
		if capability == idempotencyCapability {
			return nil
		}
	}
	return &commandError{code: "server_capability_missing", message: "Proxyma does not advertise agent command idempotency", hint: "upgrade Proxyma before using proxyma-client submit or resume", exitCode: 4}
}

func (c *Client) Resolve(ctx context.Context, target Target) (ResolvedTarget, error) {
	query := url.Values{}
	if target.Hostname != "" {
		query.Set("hostname", target.Hostname)
	}
	if target.IP != "" {
		query.Set("ip", target.IP)
	}
	var response ResolvedTarget
	err := c.request(ctx, http.MethodGet, "/v1/agent/targets/resolve?"+query.Encode(), nil, &response)
	if err == nil {
		switch response.CanonicalCommand.PayloadKind {
		case "posix_shell", "windows_shell", "powershell":
		default:
			err = proxymaInvalidResponse("Proxyma target response contained an unsupported payload kind")
		}
		if strings.TrimSpace(response.CanonicalCommand.Prefix) == "" {
			err = proxymaInvalidResponse("Proxyma target response omitted the canonical command prefix")
		}
	}
	return response, err
}

func (c *Client) Policy(ctx context.Context, target Target) (PolicyGuidance, error) {
	query := url.Values{}
	if target.Hostname != "" {
		query.Set("hostname", target.Hostname)
	}
	if target.IP != "" {
		query.Set("ip", target.IP)
	}
	var response PolicyGuidance
	if err := c.request(ctx, http.MethodGet, "/v1/agent/targets/policy?"+query.Encode(), nil, &response); err != nil {
		return PolicyGuidance{}, err
	}
	if strings.TrimSpace(response.Policy.ID) == "" {
		return PolicyGuidance{}, proxymaInvalidResponse("Proxyma policy response omitted the policy id")
	}
	return response, nil
}

func (c *Client) Submit(ctx context.Context, request commandRequest) (commandResponse, error) {
	var response commandResponse
	err := c.request(ctx, http.MethodPost, "/v1/agent/commands", request, &response)
	if err == nil && (strings.TrimSpace(response.TaskID) == "" || response.ClientOperationID != request.ClientOperationID) {
		err = proxymaInvalidResponse("Proxyma command response did not preserve the operation identity")
	}
	return response, err
}

func (c *Client) Task(ctx context.Context, taskID string) (taskResponse, error) {
	var response taskResponse
	err := c.request(ctx, http.MethodGet, "/v1/agent/tasks/"+url.PathEscape(taskID), nil, &response)
	if err == nil && (response.TaskID != taskID || strings.TrimSpace(response.Status) == "") {
		err = proxymaInvalidResponse("Proxyma task response did not match the requested task")
	}
	return response, err
}

func proxymaInvalidResponse(message string) *commandError {
	return &commandError{code: "proxyma_invalid_response", message: message, exitCode: 4}
}

func (c *Client) request(ctx context.Context, method, path string, input, output any) error {
	var body []byte
	var err error
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil {
			return &commandError{code: "invalid_input", message: err.Error(), exitCode: 2}
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		token, tokenErr := c.bearerToken(attempt > 0)
		if tokenErr != nil {
			return tokenErr
		}
		req, reqErr := http.NewRequestWithContext(ctx, method, c.cfg.ProxymaBaseURL+path, bytes.NewReader(body))
		if reqErr != nil {
			return &commandError{code: "config_error", message: reqErr.Error(), exitCode: 2}
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if input != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, doErr := c.http.Do(req)
		if doErr != nil {
			return &commandError{code: "proxyma_unreachable", message: safeNetworkError(doErr), retryable: true, hint: "retry on the next active polling interval", exitCode: 4}
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			return &commandError{code: "proxyma_error", message: "could not read Proxyma response", retryable: true, exitCode: 4}
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if output != nil && len(data) > 0 {
				if err := json.Unmarshal(data, output); err != nil {
					return &commandError{code: "proxyma_invalid_response", message: "Proxyma returned invalid JSON", exitCode: 4}
				}
			}
			return nil
		}
		var apiErr apiErrorResponse
		_ = json.Unmarshal(data, &apiErr)
		if resp.StatusCode == http.StatusUnauthorized && apiErr.Code == "token_expired" && attempt == 0 {
			continue
		}
		code := apiErr.Code
		if code == "" {
			if resp.StatusCode == http.StatusNotFound {
				code = "not_found"
			} else {
				code = "proxyma_error"
			}
		}
		message := apiErr.Error
		if message == "" {
			message = fmt.Sprintf("Proxyma returned HTTP %d", resp.StatusCode)
		}
		retryable := apiErr.Retryable || resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return &commandError{code: code, message: message, retryable: retryable, hint: apiErr.Hint, exitCode: 4}
	}
	return &commandError{code: "auth_error", message: "Proxyma authentication failed after token refresh", exitCode: 4}
}

func (c *Client) bearerToken(force bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().UTC()
	if !force && c.token != "" && now.Add(30*time.Second).Before(c.tokenUntil) {
		return c.token, nil
	}
	expires := now.Add(10 * time.Minute)
	claims := jwt.RegisteredClaims{
		ID:        uuid.NewString(),
		Subject:   c.cfg.AgentID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(expires),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["kid"] = c.cfg.AgentID
	signed, err := token.SignedString([]byte(c.cfg.SigningKey))
	if err != nil {
		return "", &commandError{code: "auth_error", message: "could not create Proxyma bearer token", exitCode: 4}
	}
	c.token = signed
	c.tokenUntil = expires
	return signed, nil
}

func safeNetworkError(err error) string {
	message := err.Error()
	for _, secret := range []string{"Authorization", "Bearer ", "agent_jwt_signing_key"} {
		message = strings.ReplaceAll(message, secret, "[redacted]")
	}
	return message
}
