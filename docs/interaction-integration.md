# Interaction Channel Integration

This document explains how to add a new Proxyma human interaction channel, such as ntfy,
WhatsApp, Slack, email, or another notification system.

## Current Boundary

Proxyma separates human interaction state from delivery. The durable state and response
validation live in `internal/interaction.Service`; channel-specific delivery lives
behind `internal/interaction.Provider`.

The important interfaces are in `internal/interaction/model.go`:

```go
type Provider interface {
	Send(ctx Context, interaction Interaction, responder Responder) (*Delivery, error)
	Cancel(ctx Context, interactionID string) error
}

type Responder interface {
	Respond(ctx Context, response Response) (*Interaction, error)
	Cancel(ctx Context, interactionID string, reason string) (*Interaction, error)
}
```

`Provider` is the interface a new channel implements. `Responder` is passed into the
provider so asynchronous channel callbacks can write normalized answers back into Proxyma.
This keeps Proxyma's task workflow independent from channel-specific APIs, webhooks, phone
numbers, device tokens, or message IDs.

## Responsibilities

A channel provider is responsible for:

- Converting an `interaction.Interaction` into a channel-specific message.
- Returning non-secret delivery metadata as `interaction.Delivery`.
- Receiving or polling for the channel response.
- Calling `responder.Respond` with a normalized `interaction.Response`.
- Calling `responder.Cancel` when the channel cannot obtain a valid answer.
- Implementing best-effort cancellation in `Cancel`.

The provider must not:

- Persist task or interaction state itself as the source of truth.
- Bypass `Responder` to modify tasks directly.
- Store secrets in `Interaction.Metadata`, `Delivery.Reference`, logs, or provider
  delivery records.
- Decide whether a task should continue after approval. That decision belongs to
  `internal/taskflow` and `internal/tasks`.

## Data Flow

1. `taskflow.Flow` decides a task needs human input.
2. It calls `interaction.Communicator.Request`.
3. `interaction.Service` validates the request, creates an `Interaction`, persists it,
   and calls `Provider.Send`.
4. The provider sends a channel-specific message and returns `Delivery`.
5. When the user replies, the provider or webhook handler calls `Responder.Respond`.
6. `interaction.Service` validates and persists the answer.
7. `taskflow.Flow` applies the answered, expired, or canceled interaction to the task.

The key design point is that channel code only handles delivery and response collection.
Proxyma owns status transitions, response validation, persistence, expiration, and task
resume behavior.

## Implementing a New Provider

Create a package under `internal/interaction/<channel>`. For example:

```text
internal/interaction/ntfy/
|-- provider.go
`-- provider_test.go
```

The package should expose a constructor that returns an `interaction.Provider`:

```go
func New(options Options) *Provider
```

Use an `Options` struct for channel configuration rather than global variables. Typical
fields are endpoint URL, topic, credentials, HTTP client, timeout, logger, and clock.
Tests should be able to inject fake clients and clocks.

Implement `Send`:

- Validate provider configuration early.
- Render only safe interaction fields into the message: `InteractionID`, `TaskID`,
  `Subject`, `Message`, `HarnessName`, and known safe metadata such as target, command,
  and reasoning.
- Include enough correlation data for the response path to recover the
  `InteractionID`.
- Return `Delivery{Provider: "<channel>", Reference: "<channel-message-id>",
  DeliveredAt: now}`.
- Return an error if the message cannot be delivered. `interaction.Service` will cancel
  the interaction when delivery fails.

Implement response handling:

- For webhook channels, add an HTTP handler or receiver component that parses the
  webhook, authenticates it, maps the channel message or payload to `InteractionID`, and
  calls `responder.Respond`.
- For polling channels, implement `interaction.PendingProvider`. Start the worker when
  the first interaction is tracked, stop it when the final interaction is untracked,
  and call `responder.Respond` for normalized replies. Tracking is also invoked for
  pending interactions recovered after an Proxyma restart.
- For command-only channels, include a reply token or explicit approval/rejection URLs
  that call back into an Proxyma endpoint.

Normalize channel responses into Proxyma responses:

```go
approved := true
_, err := responder.Respond(ctx, interaction.Response{
	InteractionID: id,
	Approved:      &approved,
	RespondedBy:   user,
})
```

For `ExpectedApproval`, set `Approved` to `true` or `false`. For `ExpectedText`, set
`Answer` to the non-empty answer text. Do not set task state directly.

Implement `Cancel`:

- Cancel or update the channel message when the provider supports it.
- Return `nil` if the channel has no cancellation primitive and Proxyma can safely treat
  cancellation as best effort.
- Do not call `Responder.Cancel` from `Provider.Cancel`; Proxyma is already canceling the
  interaction.

## Wiring

The current binary loads provider instances from the configured interaction config
directory and passes a routed provider to `interaction.Service`:

```go
interactionRegistry, _, err := instances.LoadDir(cfg.InteractionConfigDir)
provider, err := interactionProvider(interactionRegistry)
database, err := sqlite.Open(cfg.DatabasePath)
if err != nil {
	panic(err)
}
interactionService := interaction.New(database.NewInteractionStoreWithRegistry(interactionRegistry), interaction.Options{
	Provider: provider,
	Timeout:  cfg.InteractionTimeout,
})
```

To add a new non-local channel, add a provider subdirectory under
`var/proxyma/interaction_instances/`,
a loader for that provider's JSON files, and provider construction in the routed
provider registry. Keep provider-specific settings out of `proxyma.config.json`.

Non-local interaction instances include a UUID v4 `creator_user_id` for the user who
created the provider configuration. Agents select an interaction instance with
`interaction_instance_id`; non-local values are UUID v4 ids from
`var/proxyma/interaction_instances/<provider>/*.json`, and omitted values use the built-in singleton `local`
console provider. GUI users can view and edit an instance they created or an instance
referenced by an agent they own. `interaction.Service` remains unaware of
provider-specific configuration and only stores the chosen instance id on each durable
interaction.

Each agent has exactly one effective interaction instance, while an interaction instance
may be referenced by zero or more agents. In the agent GUI, a user may newly select only
`local` or an enabled instance whose `creator_user_id` matches the authenticated user.
An unchanged historical assignment owned by another user is preserved until the user
replaces it. Referenced interaction instances cannot be disabled or deleted. Provider
configuration files are separate from durable approval records in the SQLite
`interactions` table. See [database operations](database-operations.md) for
backup and reset procedures.

## Tests

Add unit tests for the provider package:

- `Send` returns useful delivery metadata and includes the expected safe message fields.
- Delivery errors are returned without hiding the cause.
- Channel replies call `Responder.Respond` with the correct `InteractionID`.
- Approval and rejection are normalized correctly.
- Invalid or unauthenticated callbacks are rejected.
- `Cancel` handles supported and unsupported cancellation behavior.

Add service or taskflow tests only when the new channel changes Proxyma behavior beyond
delivery. Most providers should not need changes outside their package and binary
wiring.

## Source Cleanup Suggestions

The interaction channel interface is mostly clean. The main improvements would make
callbacks easier to host:

- Add a documented webhook registration point if Proxyma should receive provider callbacks
  over HTTP. Today the local provider can call `Responder` in-process, but remote
  providers need an explicit callback surface.
- Consider making `interaction.Context` an alias to `context.Context` or accepting
  `context.Context` directly. The current subset is small and testable, but most
  provider libraries already use `context.Context`.
- Keep channel-specific delivery metadata in `Delivery.Reference` and avoid adding
  provider-specific fields to the core `Interaction` model unless multiple providers
  need the same concept.
