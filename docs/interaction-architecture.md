# Interaction Architecture

Proxyma separates interaction configuration, delivery, and durable approval state. Every agent has exactly one effective interaction instance: an explicit instance ID or the built-in `local` default. An interaction instance may be referenced by zero or more agents. The interaction service persists each request, and the interaction router sends it through the matching provider.

```mermaid
flowchart LR
    subgraph Configuration["Configuration and startup"]
        NtfyFiles["var/proxyma/interaction_instances/ntfy/*.json"] --> LoadInstances["instances.LoadDir"]
        LoadInstances --> Registry["instances.Registry<br/>All / Enabled / HasEnabled"]
        Registry --> Definitions["InstanceDefinition<br/>InstanceMetadata"]

        AgentFiles["var/proxyma/agents/*.json"] --> AgentRegistry["agents.Registry"]
        UserFiles["var/proxyma/users/*.json"] --> UserRegistry["users.Registry"]
        PolicyFiles["var/proxyma/policies/*.json"] --> PolicyRegistry["policy.Set"]
        LLMFiles["var/proxyma/llm_profiles/*.json"] --> LLMRegistry["llm.MutableCatalog"]
        Registry --> StartupValidation["Startup reference validation"]
        AgentRegistry --> StartupValidation
        UserRegistry --> StartupValidation
        PolicyRegistry --> StartupValidation
        LLMRegistry --> StartupValidation

        Registry --> BuildProviders["instances.BuildProviders"]
        RuntimeOptions["ProviderRuntimeOptions<br/>debug + logger"] --> BuildProviders
        BuildProviders --> LocalProvider["local.Provider<br/>built-in singleton"]
        BuildProviders --> NtfyProvider["ntfy.Provider<br/>one per enabled instance"]
        BuildProviders --> ProviderMap["map instance ID to Provider"]
        ProviderMap --> Router["instances.InteractionRouter"]
    end

    subgraph Submission["Command submission and routing"]
        AgentClient["Agent client"] --> AgentAPI["Agent command API"]
        AgentRegistry --> AgentAPI
        AgentAPI -- "copies interaction_instance_id" --> TaskManager["tasks.Manager"]
        TaskManager --> Task["Durable Task"]
        Task --> Policy["Policy evaluation<br/>deny / allow / optional LLM fallback"]
        PolicyRegistry --> Policy
        LLMRegistry --> Policy
        Policy -- "deny" --> Block["Block task"]
        Policy -- "allow" --> Execute["Execute task"]
        Policy -- "approval required" --> Flow["taskflow.Flow"]
        Flow -- "Request with instance ID" --> Service["interaction.Service<br/>Communicator + Responder"]
        Service -- "save pending record" --> InteractionStore["Interaction Store<br/>var/proxyma/interactions"]
        Service -- "Provider.Send" --> Router
        Router -- "local" --> LocalProvider
        Router -- "UUID" --> NtfyProvider
    end

    subgraph Responses["Delivery and response"]
        LocalProvider --> Console["Local console prompt"]
        Console -- "approve or reject" --> LocalProvider
        LocalProvider -- "Responder.Respond" --> Service

        NtfyProvider -- "publish approval request" --> NtfyServer["NTFY server"]
        Operator["Operator"] --> NtfyServer
        NtfyProvider -- "poll response topic" --> NtfyServer
        NtfyServer -- "normalized response" --> NtfyProvider
        NtfyProvider -- "Responder.Respond" --> Service

        Service -- "save answered / canceled / expired" --> InteractionStore
        Flow -- "Tick + Communicator.Get" --> Service
        Service -- "interaction status" --> Flow
        Flow -- "rejected, canceled, or expired" --> Block
    end

    subgraph Management["GUI interaction management"]
        GUI["Interactions page"] --> UserAPI["/v1/user/interactions"]
        UserRegistry --> UserAPI
        AgentRegistry --> UserAPI
        UserAPI -- "list owned instances<br/>admin lists all" --> Registry
        UserAPI -- "create / update / delete NTFY" --> NtfyFiles
        UserAPI -- "reload after mutation" --> LoadInstances
        UserAPI --> BuildProviders
        BuildProviders -- "complete map only" --> ReplaceProviders["InteractionRouter.SetProviders"]
        ReplaceProviders --> Router
    end
```

## Main Responsibilities

| Component | Responsibility |
| --- | --- |
| `interaction.Service` | Owns the durable interaction lifecycle: pending, answered, canceled, and expired. It implements both `Communicator` for callers and `Responder` for providers. |
| `instances.Registry` | Holds provider-neutral `InstanceDefinition` values loaded from interaction configuration files. `local` is implicit and is not a file-backed definition. |
| `instances.BuildProviders` | Converts enabled definitions into provider implementations. Provider-specific construction is contained in the interaction package. |
| `instances.InteractionRouter` | Selects a provider using `Interaction.InteractionInstanceID`, binds pending interactions to the provider generation that delivered them, and drains replaced providers before closing them. |
| `local.Provider` | Prompts on the Proxyma console and returns the operator response directly to `interaction.Service`. |
| `ntfy.Provider` | Publishes an approval message and polls the configured response topic only while it has pending interactions. Recovered pending interactions restart polling automatically. |
| `taskflow.Flow` | Requests approval when required, waits on the durable interaction status, then executes or blocks the task. |
| User management API | Manages user-owned policies, target assignments, and interaction configuration. Policy changes refresh the evaluator; interaction changes rebuild the provider map. |

Agent-authenticated HTTP clients can read interaction status but cannot answer an
interaction. Providers run inside Proxyma and return authenticated human responses through
the internal `Responder` interface, keeping command requesters separate from approvers.

## Important Data Flow

1. The authenticated agent's `interaction_instance_id` is copied into the submitted task. Missing agent configuration normalizes to `local`.
2. When policy requires approval, taskflow passes that instance ID to `interaction.Service.Request`.
3. The service persists the pending interaction before asking `InteractionRouter` to deliver it.
4. The service tracks the durable pending lifecycle with providers; answered, canceled, and expired interactions are untracked immediately.
5. The router uses the same provider generation for delivery and cancellation, including while an older configuration drains after a reload.
6. Providers return responses through the `Responder` interface; they do not approve tasks directly.

GUI users may newly assign `local` or an enabled interaction instance whose `creator_user_id` matches their user ID. Existing assignments owned by another user may be preserved while editing other agent fields, but cannot be newly selected after replacement.
7. Taskflow observes the persisted interaction status and decides whether to execute or block the task.

Policy decisions first use the deterministic blacklist and whitelist rules. A
matching blacklist rule denies the command; otherwise a matching whitelist rule
allows it. If no rule matches and the policy has an enabled LLM fallback, the
server sends the command plus the policy prompt to the selected profile. Only a
valid `approved` response allows execution; invalid, unavailable, or uncertain
responses require approval. The WebGUI/API is the only policy mutation path.
Interaction providers, including NTFY, only answer pending approval requests
and never modify policies.

Configuration reloads use build-then-swap behavior. If any provider cannot be constructed, `SetProviders` is not called and the active router configuration remains unchanged. After a successful swap, new interactions use the new providers while replaced providers continue serving their already-pending interactions and close after those interactions drain.

Historical interaction browsing and filtering is a separate GUI concern from provider
instance management. The proposed production query model is documented in
`docs/interaction-history-search-architecture.md`.
