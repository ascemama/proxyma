// Package local provides Proxyma's console-backed local interaction provider.
package local

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"proxyma/internal/interaction"
)

const ProviderName = "local"

// Provider prompts on Proxyma's local console and reads serialized y/n responses.
type Provider struct {
	mu         sync.RWMutex
	reader     *bufio.Reader
	writer     io.Writer
	now        func() time.Time
	queue      chan promptRequest
	deliveries []Delivery
	canceled   []string
}

// Delivery captures one local delivery event.
type Delivery struct {
	InteractionID string
	Kind          string
	Subject       string
	Message       string
	DeliveredAt   time.Time
}

type promptRequest struct {
	interaction interaction.Interaction
	responder   interaction.Responder
}

// New creates a console provider that reads from stdin and writes to stdout.
func New() *Provider {
	return NewWithIO(os.Stdin, os.Stdout)
}

// NewWithIO creates a provider with explicit streams for tests or embedded runtimes.
func NewWithIO(reader io.Reader, writer io.Writer) *Provider {
	provider := &Provider{
		reader: bufio.NewReader(reader),
		writer: writer,
		now:    time.Now,
		queue:  make(chan promptRequest, 64),
	}
	go provider.run()
	return provider
}

// Send records and queues the interaction for local console prompting.
func (p *Provider) Send(_ interaction.Context, request interaction.Interaction, responder interaction.Responder) (*interaction.Delivery, error) {
	deliveredAt := p.now().UTC()
	p.mu.Lock()
	p.deliveries = append(p.deliveries, Delivery{
		InteractionID: request.InteractionID,
		Kind:          request.Kind,
		Subject:       request.Subject,
		Message:       request.Message,
		DeliveredAt:   deliveredAt,
	})
	p.mu.Unlock()

	select {
	case p.queue <- promptRequest{interaction: request, responder: responder}:
	default:
		return nil, fmt.Errorf("local interaction prompt queue is full")
	}
	return &interaction.Delivery{
		Provider:    ProviderName,
		Reference:   request.InteractionID,
		DeliveredAt: deliveredAt,
	}, nil
}

// Cancel records a cancellation for a delivered interaction.
func (p *Provider) Cancel(_ interaction.Context, request interaction.Interaction) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.canceled = append(p.canceled, request.InteractionID)
	return nil
}

// Deliveries returns recorded deliveries.
func (p *Provider) Deliveries() []Delivery {
	p.mu.RLock()
	defer p.mu.RUnlock()
	copied := make([]Delivery, len(p.deliveries))
	copy(copied, p.deliveries)
	return copied
}

// Canceled returns recorded cancellation interaction IDs.
func (p *Provider) Canceled() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	copied := make([]string, len(p.canceled))
	copy(copied, p.canceled)
	return copied
}

func (p *Provider) run() {
	for request := range p.queue {
		p.prompt(request)
	}
}

func (p *Provider) prompt(request promptRequest) {
	p.writePrompt(request.interaction)
	for {
		if _, err := fmt.Fprint(p.writer, "Approve? [y/n]: "); err != nil {
			_, _ = request.responder.Cancel(context.Background(), request.interaction.InteractionID, "local console output failed: "+err.Error())
			return
		}
		line, err := p.reader.ReadString('\n')
		answer := strings.ToLower(strings.TrimSpace(line))
		switch answer {
		case "y", "yes":
			approved := true
			_, _ = request.responder.Respond(context.Background(), interaction.Response{
				InteractionID: request.interaction.InteractionID,
				Approved:      &approved,
				RespondedBy:   "local-console",
			})
			return
		case "n", "no":
			approved := false
			_, _ = request.responder.Respond(context.Background(), interaction.Response{
				InteractionID: request.interaction.InteractionID,
				Approved:      &approved,
				RespondedBy:   "local-console",
			})
			return
		case "":
			if err != nil {
				p.cancelForReadError(request, err)
				return
			}
		default:
			_, _ = fmt.Fprintln(p.writer, "Please answer y or n.")
		}
		if err != nil {
			p.cancelForReadError(request, err)
			return
		}
	}
}

func (p *Provider) writePrompt(request interaction.Interaction) {
	_, _ = fmt.Fprintln(p.writer)
	_, _ = fmt.Fprintln(p.writer, "Proxyma command approval required")
	_, _ = fmt.Fprintf(p.writer, "Interaction ID: %s\n", request.InteractionID)
	if strings.TrimSpace(request.TaskID) != "" {
		_, _ = fmt.Fprintf(p.writer, "Task ID: %s\n", request.TaskID)
	}
	if target := stringMetadata(request.Metadata, "target"); target != "" {
		_, _ = fmt.Fprintf(p.writer, "Target: %s\n", target)
	}
	if command := stringMetadata(request.Metadata, "command"); command != "" {
		_, _ = fmt.Fprintf(p.writer, "Command: %s\n", command)
	}
	if reasoning := stringMetadata(request.Metadata, "reasoning"); reasoning != "" {
		_, _ = fmt.Fprintf(p.writer, "Reasoning: %s\n", reasoning)
	}
	if requestedBy := strings.TrimSpace(request.RequestedBy); requestedBy != "" {
		_, _ = fmt.Fprintf(p.writer, "Requested by: %s\n", requestedBy)
	}
	_, _ = fmt.Fprintln(p.writer)
}

func (p *Provider) cancelForReadError(request promptRequest, err error) {
	reason := "local console input failed: " + err.Error()
	if err == io.EOF {
		reason = "local console input closed"
	}
	_, _ = request.responder.Cancel(context.Background(), request.interaction.InteractionID, reason)
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
