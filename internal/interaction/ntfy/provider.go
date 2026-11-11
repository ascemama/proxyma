// Package ntfy provides an ntfy-backed Proxyma interaction provider.
package ntfy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"proxyma/internal/interaction"
)

const ProviderName = "ntfy"

const (
	defaultRespondedBy = "ntfy"
)

// Options configures the ntfy interaction provider.
type Options struct {
	BaseURL       string
	RequestTopic  string
	ResponseTopic string
	Token         string
	PollInterval  time.Duration
	PollTimeout   time.Duration
	RespondedBy   string
	HTTPClient    *http.Client
	Now           func() time.Time
	Debug         bool
	Logger        *log.Logger
}

// Provider sends approval notifications through ntfy and polls for action replies.
type Provider struct {
	baseURL       string
	requestTopic  string
	responseTopic string
	token         string
	pollInterval  time.Duration
	pollTimeout   time.Duration
	respondedBy   string
	client        *http.Client
	now           func() time.Time
	debug         bool
	logger        *log.Logger

	mu                 sync.Mutex
	lastSeen           string
	seenMessages       map[string]struct{}
	activeResponder    interaction.Responder
	activeInteractions map[string]struct{}
	pollCancel         context.CancelFunc
	pollGeneration     uint64
	closed             bool
}

type publishRequest struct {
	Topic   string          `json:"topic"`
	Title   string          `json:"title,omitempty"`
	Message string          `json:"message"`
	Actions []publishAction `json:"actions,omitempty"`
}

type publishAction struct {
	Action string `json:"action"`
	Label  string `json:"label"`
	URL    string `json:"url"`
	Method string `json:"method,omitempty"`
	Body   string `json:"body,omitempty"`
	Clear  bool   `json:"clear,omitempty"`
}

type ntfyEvent struct {
	ID      string `json:"id"`
	Time    int64  `json:"time"`
	Event   string `json:"event"`
	Topic   string `json:"topic"`
	Message string `json:"message"`
}

type approvalResponse struct {
	InteractionID string `json:"interaction_id"`
	Approved      *bool  `json:"approved,omitempty"`
	RespondedBy   string `json:"responded_by,omitempty"`
}

// New creates an ntfy provider. A zero PollInterval disables the background poller.
func New(options Options) (*Provider, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(options.BaseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("ntfy base URL is required")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("ntfy base URL must be an absolute HTTP URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("ntfy base URL must use http or https")
	}
	requestTopic := strings.TrimSpace(options.RequestTopic)
	if requestTopic == "" {
		return nil, fmt.Errorf("ntfy request topic is required")
	}
	responseTopic := strings.TrimSpace(options.ResponseTopic)
	if responseTopic == "" {
		return nil, fmt.Errorf("ntfy response topic is required")
	}
	if options.PollInterval < 0 {
		return nil, fmt.Errorf("ntfy poll interval cannot be negative")
	}
	if options.PollTimeout < 0 {
		return nil, fmt.Errorf("ntfy poll timeout cannot be negative")
	}
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	respondedBy := strings.TrimSpace(options.RespondedBy)
	if respondedBy == "" {
		respondedBy = defaultRespondedBy
	}
	return &Provider{
		baseURL:            baseURL,
		requestTopic:       requestTopic,
		responseTopic:      responseTopic,
		token:              strings.TrimSpace(options.Token),
		pollInterval:       options.PollInterval,
		pollTimeout:        options.PollTimeout,
		respondedBy:        respondedBy,
		client:             client,
		now:                now,
		debug:              options.Debug,
		logger:             options.Logger,
		seenMessages:       make(map[string]struct{}),
		activeInteractions: make(map[string]struct{}),
	}, nil
}

// Send publishes an approval prompt and starts polling for responses.
func (p *Provider) Send(ctx interaction.Context, request interaction.Interaction, responder interaction.Responder) (*interaction.Delivery, error) {
	payload, err := p.buildPublishRequest(request)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	httpCtx, cancel := p.withTimeout(ctx)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(httpCtx, http.MethodPost, p.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	p.authorize(httpReq)
	p.debugHTTPRequest(httpReq, body)
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ntfy publish failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ntfy publish failed: status %d", resp.StatusCode)
	}
	reference := request.InteractionID
	var published ntfyEvent
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&published); err == nil && strings.TrimSpace(published.ID) != "" {
		reference = published.ID
	}
	deliveredAt := p.now().UTC()
	return &interaction.Delivery{
		Provider:    ProviderName,
		Reference:   reference,
		DeliveredAt: deliveredAt,
	}, nil
}

// Cancel is best effort for ntfy v1; ntfy does not provide a portable cancellation primitive.
func (p *Provider) Cancel(_ interaction.Context, _ interaction.Interaction) error {
	return nil
}

func (p *Provider) buildPublishRequest(request interaction.Interaction) (publishRequest, error) {
	approveBody, err := marshalApprovalBody(request.InteractionID, true)
	if err != nil {
		return publishRequest{}, err
	}
	rejectBody, err := marshalApprovalBody(request.InteractionID, false)
	if err != nil {
		return publishRequest{}, err
	}
	responseURL := p.topicURL(p.responseTopic)
	return publishRequest{
		Topic:   p.requestTopic,
		Title:   "Approve Proxyma command",
		Message: renderMessage(request),
		Actions: []publishAction{
			{Action: "http", Label: "Approve", URL: responseURL, Method: http.MethodPost, Body: approveBody, Clear: true},
			{Action: "http", Label: "Reject", URL: responseURL, Method: http.MethodPost, Body: rejectBody, Clear: true},
		},
	}, nil
}

func marshalApprovalBody(interactionID string, approved bool) (string, error) {
	body, err := json.Marshal(approvalResponse{
		InteractionID: interactionID,
		Approved:      &approved,
	})
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func renderMessage(request interaction.Interaction) string {
	var lines []string
	if strings.TrimSpace(request.TaskID) != "" {
		lines = append(lines, "Task ID: "+strings.TrimSpace(request.TaskID))
	}
	if target := stringMetadata(request.Metadata, "target"); target != "" {
		lines = append(lines, "Target: "+target)
	}
	if command := stringMetadata(request.Metadata, "command"); command != "" {
		lines = append(lines, "Command: "+command)
	}
	if reasoning := stringMetadata(request.Metadata, "reasoning"); reasoning != "" {
		lines = append(lines, "Reasoning: "+reasoning)
	}
	if justification := stringMetadata(request.Metadata, "policy_justification"); justification != "" {
		lines = append(lines, "LLM justification: "+justification)
	}
	if harnessName := strings.TrimSpace(request.HarnessName); harnessName != "" {
		lines = append(lines, "Harness name: "+harnessName)
	}
	if len(lines) == 0 {
		return strings.TrimSpace(request.Message)
	}
	return strings.Join(lines, "\n")
}

func stringMetadata(metadata map[string]any, key string) string {
	value, ok := metadata[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

// TrackPending starts polling when the first interaction becomes pending.
func (p *Provider) TrackPending(request interaction.Interaction, responder interaction.Responder) {
	if responder == nil || p.pollInterval <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.activeInteractions[request.InteractionID] = struct{}{}
	p.activeResponder = responder
	if p.pollCancel == nil {
		p.startPollerLocked()
	}
}

// UntrackPending stops polling when the final pending interaction completes.
func (p *Provider) UntrackPending(request interaction.Interaction) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.activeInteractions, request.InteractionID)
	if len(p.activeInteractions) == 0 && p.pollCancel != nil {
		p.pollCancel()
	}
}

// Close permanently stops this provider's poller.
func (p *Provider) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	clear(p.activeInteractions)
	if p.pollCancel != nil {
		p.pollCancel()
	}
}

func (p *Provider) startPollerLocked() {
	ctx, cancel := context.WithCancel(context.Background())
	p.pollCancel = cancel
	p.pollGeneration++
	generation := p.pollGeneration
	go p.pollLoop(ctx, generation)
}

func (p *Provider) pollLoop(ctx context.Context, generation uint64) {
	defer func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.pollGeneration != generation {
			return
		}
		p.pollCancel = nil
		if !p.closed && len(p.activeInteractions) > 0 {
			p.startPollerLocked()
		}
	}()
	for {
		responder := p.responder()
		if responder != nil {
			_ = p.pollOnce(ctx, responder)
		}
		timer := time.NewTimer(p.pollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
}

func (p *Provider) responder() interaction.Responder {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.activeResponder
}

func (p *Provider) pollOnce(ctx context.Context, responder interaction.Responder) error {
	httpCtx, cancel := p.withContextTimeout(ctx)
	defer cancel()
	pollURL, err := p.pollURL()
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(httpCtx, http.MethodGet, pollURL, nil)
	if err != nil {
		return err
	}
	p.authorize(req)
	p.debugHTTPRequest(req, nil)
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("ntfy poll failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ntfy poll failed: status %d", resp.StatusCode)
	}
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event ntfyEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return fmt.Errorf("ntfy poll returned invalid JSON: %w", err)
		}
		if err := p.handleEvent(ctx, responder, event); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("ntfy poll read failed: %w", err)
	}
	return nil
}

func (p *Provider) handleEvent(ctx context.Context, responder interaction.Responder, event ntfyEvent) error {
	if event.ID != "" && p.alreadySeen(event.ID) {
		return nil
	}
	if event.ID != "" {
		p.remember(event.ID)
	}
	if event.Event != "message" {
		return nil
	}
	var response approvalResponse
	if err := json.Unmarshal([]byte(event.Message), &response); err != nil {
		return nil
	}
	if strings.TrimSpace(response.InteractionID) == "" || response.Approved == nil {
		return nil
	}
	respondedBy := strings.TrimSpace(response.RespondedBy)
	if respondedBy == "" {
		respondedBy = p.respondedBy
	}
	var respondedAt *time.Time
	if event.Time > 0 {
		value := time.Unix(event.Time, 0).UTC()
		respondedAt = &value
	}
	_, err := responder.Respond(ctx, interaction.Response{
		InteractionID: strings.TrimSpace(response.InteractionID),
		Approved:      response.Approved,
		RespondedBy:   respondedBy,
		RespondedAt:   respondedAt,
	})
	if err == interaction.ErrNotFound {
		return nil
	}
	return err
}

func (p *Provider) pollURL() (string, error) {
	u, err := url.Parse(p.topicURL(p.responseTopic) + "/json")
	if err != nil {
		return "", err
	}
	query := u.Query()
	query.Set("poll", "1")
	if since := p.since(); since != "" {
		query.Set("since", since)
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func (p *Provider) topicURL(topic string) string {
	return p.baseURL + "/" + url.PathEscape(topic)
}

func (p *Provider) since() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.lastSeen == "" {
		return "all"
	}
	return p.lastSeen
}

func (p *Provider) alreadySeen(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.seenMessages[id]
	return ok
}

func (p *Provider) remember(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastSeen = id
	p.seenMessages[id] = struct{}{}
}

func (p *Provider) authorize(req *http.Request) {
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}
}

func (p *Provider) withTimeout(ctx interaction.Context) (context.Context, context.CancelFunc) {
	return p.withContextTimeout(contextFrom(ctx))
}

func (p *Provider) withContextTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if p.pollTimeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, p.pollTimeout)
}

func contextFrom(ctx interaction.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	if concrete, ok := ctx.(context.Context); ok {
		return concrete
	}
	return context.Background()
}

func (p *Provider) debugHTTPRequest(req *http.Request, body []byte) {
	if !p.debug {
		return
	}
	logger := p.logger
	if logger == nil {
		logger = log.Default()
	}
	if len(body) == 0 {
		logger.Printf("debug ntfy request method=%s url=%s", req.Method, req.URL.String())
		return
	}
	logger.Printf("debug ntfy request method=%s url=%s body=%s", req.Method, req.URL.String(), string(body))
}
