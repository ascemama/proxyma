# Proxyma Typical Usage Flow

This editable Mermaid flow diagram shows what each Proxyma component does during a
typical harness-submitted command that is evaluated by target-owned command
policy before execution.

## Taskflow vs Tasks

`internal/taskflow` is the lifecycle policy layer. It looks at durable task
state, target-owned command policy, and interaction state, then decides the next
high-level transition: start directly, deny, request approval, wait for an
answer, apply an approval/rejection/expiration, or ask the task manager to
start/block a task. It also owns the scheduler loop that periodically invokes
those lifecycle decisions. It does not own task storage, task JSON shape, or
command execution.

`internal/tasks` is the task domain and runtime primitive layer. It owns the
durable `Task` model, task status constants, status history, approval fields,
policy decision fields, persistence-facing task operations, command execution,
and result recording.

The code is separated so policy decisions stay outside the task persistence and
execution primitive. That lets `tasks.Manager` remain the authoritative mutator
for task records, while `taskflow.Flow` can express Proxyma-specific lifecycle
policy such as "this target's policy allows, denies, or requires approval"
without embedding that policy into every low-level task operation. It also keeps
taskflow scheduling close to the lifecycle policy it periodically invokes.

## Flow vs Scheduler vs Manager

The flow, scheduler, and manager have different roles:

- `tasks.Manager` owns task state changes and command execution. It creates
  tasks, loads and recovers persisted tasks, transitions tasks to waiting/running/
  final states, applies answered interactions to the task record, starts the
  command goroutine, and persists results.
- `taskflow.Flow` owns Proxyma lifecycle decisions. It reads task, policy, and
  interaction state, requests approval when needed, and calls the manager to
  start or block tasks.
- `taskflow.Scheduler` owns timing and concurrency for lifecycle passes. It runs
  a loop, prevents overlapping passes, and calls `StartCreated` followed by
  `ManageState` on its configured lifecycle controller. It does not inspect task
  details directly and does not execute commands itself.

In `cmd/proxyma/main.go`, the runtime wiring is:

```text
tasks.Manager -> taskflow.Flow -> taskflow.Scheduler
```

The manager supplies the durable task operations, the flow supplies the lifecycle
decisions, and the scheduler supplies the periodic trigger.

```mermaid
flowchart TD
    Start(["START<br/>User asks harness to run a target command"])

    subgraph Harness["Harness / Agent"]
        H1["Build Proxyma command request<br/>Data: target hostname/IP, canonical command, reasoning, requested_by"]
        H2["Receive task_id immediately<br/>Data: task_id, status=created"]
        H3["Poll task status<br/>Data: task_id"]
        H4["Record final result in harness task state<br/>Data: stdout, stderr, exit_status, status"]
    end

    subgraph API["internal/api"]
        A1["Authenticate bearer token"]
        A2["Decode and validate POST /v1/agent/commands"]
        A3["Match explicit target selector<br/>Data: hostname/IP only"]
        A7["Authorize agent for target/task"]
        A4["Prepare command context for task creation"]
        A5["Return 202 Accepted"]
        A6["Serve GET /v1/agent/tasks/{task_id}"]
    end

    subgraph Transcriptor["internal/transcriptor"]
        TR1["Parse canonical SSH command"]
        TR2["Build internal execution plan<br/>Data: SSH request, safe transcription metadata"]
    end

    subgraph TasksManager["internal/tasks.Manager"]
        T1["Create durable task<br/>State: created"]
        T0["Persist allow policy decision<br/>Data: decision=allow"]
        T0A["Persist approval policy decision<br/>Data: decision=approval"]
        T2["Mark task waiting for user response<br/>State: waiting_user_response"]
        T3["Record user approval<br/>State: accepted_by_user<br/>Data: approval status, interaction_id"]
        T4["Start accepted task<br/>State: running"]
        T5["Persist final task result<br/>State: completed / failed / timed_out / blocked"]
    end

    subgraph TaskStore["internal/store: task records"]
        TS1[("Task JSON<br/>canonical command, target, policy decision, status, history, safe metadata")]
    end

    subgraph Policy["internal/taskflow/policy"]
        P1["Evaluate hard deny rules<br/>Data: normalized remote command"]
        P2{"Denied?"}
        P3["Match deterministic allow rules<br/>Data: typed command templates and validated arguments"]
        P4{"Allowed?"}
        P5{"LLM evaluator enabled?"}
        P6["Classify command against configured policy<br/>Data: sanitized command context"]
        P7{"LLM decision?"}
    end

    subgraph Scheduler["internal/taskflow.Scheduler"]
        S1["Trigger lifecycle pass<br/>Calls StartCreated then ManageState"]
        S2["Trigger next lifecycle pass<br/>Skips if previous pass still running"]
    end

    subgraph Taskflow["internal/taskflow"]
        TF1["Decide what to do with created/accepted tasks"]
        TF2["Request policy decision"]
        TF3["Ask interaction service for approval request"]
        TF4["Decide what to do with waiting tasks"]
        TF5{"Interaction answered?"}
        TF6{"Approved?"}
        TF7["Call tasks.Manager.StartTask"]
        TF8["Call tasks.Manager.BlockTask"]
    end

    subgraph Interaction["internal/interaction.Service"]
        I1["Create pending approval interaction<br/>Data: task_id, kind=approval_request, safe prompt metadata"]
        I2["Persist pending interaction"]
        I3["Receive provider response<br/>Data: approved true/false"]
        I4["Persist answered interaction"]
    end

    subgraph InteractionStore["internal/store: interaction records"]
        IS1[("Interaction JSON<br/>status, response, delivery metadata")]
    end

    subgraph LocalProvider["internal/interaction/local"]
        L1["Print approval prompt on Proxyma console<br/>Data shown: task_id, target, command, reasoning, requested_by"]
        L2["Read y/n from stdin"]
        L3["Send normalized response back to interaction service"]
    end

    subgraph Executor["internal/sshclient / internal/executor"]
        E1["Execute approved command<br/>SSH plan or fallback executor"]
        E2["Return execution result<br/>Data: stdout, stderr, exit_status, timeout/error"]
    end

    Start -->|"1. submit command"| H1
    H1 -->|"HTTP request"| A1
    A1 --> A2
    A2 --> A3
    A3 --> A7
    A7 --> TR1
    TR1 --> TR2
    TR2 -->|"execution plan + transcription metadata"| A4
    A4 --> T1
    T1 -->|"write task"| TS1
    T1 --> A5
    A5 -->|"202 + task_id"| H2

    S1 -->|"scheduler invokes taskflow.StartCreated"| TF1
    TF1 --> TF2
    TF2 --> P1
    P1 --> P2
    P2 -->|"yes"| TF8
    P2 -->|"no"| P3
    P3 --> P4
    P4 -->|"yes"| T0
    T0 -->|"write policy"| TS1
    T0 --> TF7
    P4 -->|"no"| P5
    P5 -->|"yes"| P6
    P5 -->|"no"| T0A
    P6 --> P7
    P7 -->|"allow within configured safety boundary"| T0
    P7 -->|"deny"| TF8
    P7 -->|"approval / unavailable / low confidence"| T0A
    T0A -->|"write policy"| TS1
    T0A --> TF3
    TF3 --> I1
    I1 --> I2
    I2 -->|"write interaction"| IS1
    I1 --> L1
    L1 --> L2
    TF3 --> T2
    T2 -->|"update task status/history"| TS1
    L2 --> L3
    L3 --> I3
    I3 --> I4
    I4 -->|"update interaction"| IS1

    S2 -->|"scheduler invokes taskflow.ManageState"| TF4
    TF4 --> TF5
    TF5 -->|"yes"| TF6
    TF6 -->|"yes"| T3
    T3 --> TF7
    TF7 --> T4
    T4 --> E1
    E1 --> E2
    E2 --> T5
    T5 -->|"write final task"| TS1
    TF6 -->|"no"| TF8
    TF8 --> T5

    H3 -->|"HTTP poll"| A6
    A6 -->|"read task"| TS1
    A6 -->|"task status response"| H3
    H3 -->|"final status observed"| H4

    TF5 -->|"no"| TF4
```

## Data Boundaries

- Harness-visible data stays canonical and secret-safe: target hostname/IP, submitted command, reasoning, policy decision, task status, owning agent ID, and result.
- Authenticated agents can submit commands only to targets that list their ID in `agent_ids`, and can read only their own task and task-linked interaction records.
- `internal/transcriptor` creates the internal execution plan, but task status and stored JSON expose only safe transcription metadata.
- `internal/interaction/local` displays only safe prompt fields on the Proxyma console.
- Task records and interaction records are persisted separately through `internal/store`.
- `internal/taskflow` reads task and interaction state to choose lifecycle
  transitions; `internal/tasks.Manager` performs the task mutation and
  persistence for those transitions.
- `internal/taskflow.Scheduler` is deliberately state-light: it only triggers
  lifecycle passes and reports pass counts.
