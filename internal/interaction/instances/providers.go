package instances

import (
	"fmt"
	"log"

	"proxyma/internal/interaction"
	interactionlocal "proxyma/internal/interaction/local"
	interactionntfy "proxyma/internal/interaction/ntfy"
)

// ProviderRuntimeOptions contains dependencies shared by interaction providers.
type ProviderRuntimeOptions struct {
	Debug  bool
	Logger *log.Logger
}

// BuildProviders constructs the complete provider registry for enabled instances.
func BuildProviders(registry Registry, runtime ProviderRuntimeOptions) (map[string]interaction.Provider, error) {
	providers := map[string]interaction.Provider{
		interaction.DefaultInstanceLocal: interactionlocal.New(),
	}
	for _, definition := range registry.Enabled() {
		metadata := definition.Metadata()
		switch instance := definition.(type) {
		case NtfyInstance:
			options := instance.NtfyOptions()
			options.Debug = runtime.Debug
			options.Logger = runtime.Logger
			provider, err := interactionntfy.New(options)
			if err != nil {
				return nil, fmt.Errorf("configure %s interaction instance %q: %w", metadata.ProviderType, metadata.ID, err)
			}
			providers[metadata.ID] = provider
		default:
			return nil, fmt.Errorf("configure %s interaction instance %q: unsupported definition type %T", metadata.ProviderType, metadata.ID, definition)
		}
	}
	return providers, nil
}
