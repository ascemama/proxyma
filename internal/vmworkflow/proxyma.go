package vmworkflow

import (
	"context"

	"proxyma/internal/clientcli"
)

type Proxyma interface {
	Resolve(ctx context.Context, target clientcli.Target) (clientcli.Envelope, error)
	Submit(ctx context.Context, input clientcli.SubmitInput) (clientcli.Envelope, error)
	Resume(ctx context.Context) (clientcli.Envelope, error)
}

type clientProxyma struct{ service *clientcli.Service }

func newClientProxyma(configPath string) (*clientProxyma, error) {
	config, err := clientcli.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	return &clientProxyma{service: clientcli.NewService(config)}, nil
}

func (p *clientProxyma) Resolve(ctx context.Context, target clientcli.Target) (clientcli.Envelope, error) {
	return p.service.WithStateLock(func() (clientcli.Envelope, error) { return p.service.InspectTarget(ctx, target) })
}

func (p *clientProxyma) Submit(ctx context.Context, input clientcli.SubmitInput) (clientcli.Envelope, error) {
	return p.service.WithStateLock(func() (clientcli.Envelope, error) { return p.service.Submit(ctx, input) })
}

func (p *clientProxyma) Resume(ctx context.Context) (clientcli.Envelope, error) {
	return p.service.WithStateLock(func() (clientcli.Envelope, error) { return p.service.Resume(ctx) })
}
