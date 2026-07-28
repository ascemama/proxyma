package instances

import (
	"fmt"
	"sync"

	"proxyma/internal/interaction"
)

// InteractionRouter dispatches interactions to the provider configured for each instance id.
type InteractionRouter struct {
	mu        sync.RWMutex
	providers map[string]*routedProvider
	bindings  map[string]*providerBinding
}

type routedProvider struct {
	provider interaction.Provider
	active   int
	retired  bool
}

type providerBinding struct {
	state   *routedProvider
	tracked bool
}

// NewInteractionRouter creates an instance-aware interaction provider.
func NewInteractionRouter(providers map[string]interaction.Provider) *InteractionRouter {
	router := &InteractionRouter{bindings: make(map[string]*providerBinding)}
	router.SetProviders(providers)
	return router
}

// SetProviders replaces the routed provider registry.
func (r *InteractionRouter) SetProviders(providers map[string]interaction.Provider) {
	copied := make(map[string]*routedProvider, len(providers))
	for id, provider := range providers {
		normalized := NormalizeInstanceID(id)
		if provider != nil {
			copied[normalized] = &routedProvider{provider: provider}
		}
	}
	r.mu.Lock()
	var closeAfter []*routedProvider
	for _, state := range r.providers {
		state.retired = true
		if state.active == 0 {
			closeAfter = append(closeAfter, state)
		}
	}
	r.providers = copied
	r.mu.Unlock()
	for _, state := range closeAfter {
		closeProvider(state.provider)
	}
}

// Send routes delivery to the request's configured interaction instance.
func (r *InteractionRouter) Send(ctx interaction.Context, request interaction.Interaction, responder interaction.Responder) (*interaction.Delivery, error) {
	state, binding, err := r.reserve(request)
	if err != nil {
		return nil, err
	}
	delivery, err := state.provider.Send(ctx, request, responder)
	if err != nil {
		r.release(request.InteractionID, binding)
		return nil, err
	}
	r.mu.Lock()
	if r.bindings[request.InteractionID] == binding {
		if provider, ok := state.provider.(interaction.PendingProvider); ok {
			provider.TrackPending(request, responder)
			binding.tracked = true
		}
	}
	r.mu.Unlock()
	return delivery, nil
}

// Cancel routes cancellation to the same interaction instance used for delivery.
func (r *InteractionRouter) Cancel(ctx interaction.Context, request interaction.Interaction) error {
	provider, err := r.boundProvider(request)
	if err != nil {
		return err
	}
	return provider.Cancel(ctx, request)
}

// TrackPending restores a durable pending interaction after service recovery.
func (r *InteractionRouter) TrackPending(request interaction.Interaction, responder interaction.Responder) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if binding, exists := r.bindings[request.InteractionID]; exists {
		if !binding.tracked {
			if provider, ok := binding.state.provider.(interaction.PendingProvider); ok {
				provider.TrackPending(request, responder)
				binding.tracked = true
			}
		}
		return
	}
	state, ok := r.providers[NormalizeInstanceID(request.InteractionInstanceID)]
	if !ok {
		return
	}
	binding := &providerBinding{state: state}
	r.bindings[request.InteractionID] = binding
	state.active++
	if provider, ok := state.provider.(interaction.PendingProvider); ok {
		provider.TrackPending(request, responder)
		binding.tracked = true
	}
}

// UntrackPending releases the provider generation bound to a terminal interaction.
func (r *InteractionRouter) UntrackPending(request interaction.Interaction) {
	r.mu.Lock()
	binding, ok := r.bindings[request.InteractionID]
	if !ok {
		r.mu.Unlock()
		return
	}
	delete(r.bindings, request.InteractionID)
	if binding.tracked {
		if provider, ok := binding.state.provider.(interaction.PendingProvider); ok {
			provider.UntrackPending(request)
		}
	}
	binding.state.active--
	closeAfter := binding.state.retired && binding.state.active == 0
	r.mu.Unlock()
	if closeAfter {
		closeProvider(binding.state.provider)
	}
}

// Close releases every current and draining provider.
func (r *InteractionRouter) Close() {
	r.mu.Lock()
	states := make(map[*routedProvider]struct{})
	for _, state := range r.providers {
		states[state] = struct{}{}
	}
	for _, binding := range r.bindings {
		states[binding.state] = struct{}{}
	}
	r.providers = nil
	r.bindings = make(map[string]*providerBinding)
	r.mu.Unlock()
	for state := range states {
		closeProvider(state.provider)
	}
}

func (r *InteractionRouter) providerFor(id string) (interaction.Provider, error) {
	normalized := NormalizeInstanceID(id)
	if r == nil {
		return nil, fmt.Errorf("interaction provider registry is not configured")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r == nil || r.providers == nil {
		return nil, fmt.Errorf("interaction provider registry is not configured")
	}
	state, ok := r.providers[normalized]
	if !ok {
		return nil, fmt.Errorf("interaction instance %q is not configured", normalized)
	}
	return state.provider, nil
}

func (r *InteractionRouter) reserve(request interaction.Interaction) (*routedProvider, *providerBinding, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r == nil || r.providers == nil {
		return nil, nil, fmt.Errorf("interaction provider registry is not configured")
	}
	instanceID := NormalizeInstanceID(request.InteractionInstanceID)
	state, ok := r.providers[instanceID]
	if !ok {
		return nil, nil, fmt.Errorf("interaction instance %q is not configured", instanceID)
	}
	if binding, exists := r.bindings[request.InteractionID]; exists {
		return binding.state, binding, nil
	}
	binding := &providerBinding{state: state}
	r.bindings[request.InteractionID] = binding
	state.active++
	return state, binding, nil
}

func (r *InteractionRouter) release(interactionID string, binding *providerBinding) {
	r.mu.Lock()
	if r.bindings[interactionID] != binding {
		r.mu.Unlock()
		return
	}
	delete(r.bindings, interactionID)
	if binding.tracked {
		if provider, ok := binding.state.provider.(interaction.PendingProvider); ok {
			provider.UntrackPending(interaction.Interaction{InteractionID: interactionID})
		}
	}
	binding.state.active--
	closeAfter := binding.state.retired && binding.state.active == 0
	r.mu.Unlock()
	if closeAfter {
		closeProvider(binding.state.provider)
	}
}

func (r *InteractionRouter) boundProvider(request interaction.Interaction) (interaction.Provider, error) {
	r.mu.RLock()
	if binding, ok := r.bindings[request.InteractionID]; ok {
		provider := binding.state.provider
		r.mu.RUnlock()
		return provider, nil
	}
	r.mu.RUnlock()
	return r.providerFor(request.InteractionInstanceID)
}

func closeProvider(provider interaction.Provider) {
	if managed, ok := provider.(interaction.PendingProvider); ok {
		managed.Close()
	}
}
