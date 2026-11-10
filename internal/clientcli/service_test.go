package clientcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type fakeProxyma struct {
	server      *httptest.Server
	mu          sync.Mutex
	submissions int
	lastCommand commandRequest
	taskStatus  string
}

func newFakeProxyma(t *testing.T) *fakeProxyma {
	t.Helper()
	fake := &fakeProxyma{taskStatus: "running"}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Fatalf("missing bearer token")
		}
		switch {
		case r.URL.Path == "/v1/health":
			writeTestJSON(w, map[string]any{"status": "ok", "capabilities": []string{idempotencyCapability}})
		case r.URL.Path == "/v1/agent/targets/resolve":
			writeTestJSON(w, map[string]any{"target": map[string]any{"transport": "ssh", "os": "linux", "hostname": "example.test"}, "canonical_command": map[string]any{"prefix": "ssh {user}@example.test ", "payload_kind": "posix_shell"}})
		case r.URL.Path == "/v1/agent/commands":
			fake.mu.Lock()
			fake.submissions++
			_ = json.NewDecoder(r.Body).Decode(&fake.lastCommand)
			fake.mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
			writeTestJSON(w, map[string]any{"task_id": "proxyma-task-1", "status": "created", "client_operation_id": fake.lastCommand.ClientOperationID})
		case strings.HasPrefix(r.URL.Path, "/v1/agent/tasks/"):
			fake.mu.Lock()
			status := fake.taskStatus
			operationID := fake.lastCommand.ClientOperationID
			fake.mu.Unlock()
			response := map[string]any{"task_id": "proxyma-task-1", "client_operation_id": operationID, "status": status}
			if status == "completed" {
				response["result"] = map[string]any{"stdout": "ok\n", "stderr": "", "exit_status": 0, "truncated": false}
			}
			writeTestJSON(w, response)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func writeTestJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func TestSubmitAndResumeCompletedTask(t *testing.T) {
	fake := newFakeProxyma(t)
	service := NewService(testConfig(t, fake.server.URL))
	var submitted Envelope
	var err error
	submitted, err = service.WithStateLock(func() (Envelope, error) {
		return service.Submit(context.Background(), SubmitInput{
			UserGoal:    "identify the host",
			Target:      Target{Hostname: "example.test"},
			Payload:     "hostname",
			Reasoning:   "identify the target host",
			HarnessName: "test-agent",
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if submitted.Action != "wait" || submitted.Task == nil || submitted.Task.Status != "waiting_proxyma" {
		t.Fatalf("submit envelope = %#v", submitted)
	}
	fake.mu.Lock()
	if fake.submissions != 1 || fake.lastCommand.Command != "ssh {user}@example.test hostname" || fake.lastCommand.ClientOperationID == "" {
		t.Fatalf("submission count=%d request=%#v", fake.submissions, fake.lastCommand)
	}
	fake.taskStatus = "completed"
	fake.mu.Unlock()
	resumed, err := service.WithStateLock(func() (Envelope, error) { return service.Resume(context.Background()) })
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Action != "continue" || len(resumed.Tasks) != 1 || resumed.Tasks[0].Status != "pending" || resumed.Tasks[0].Operations[0].Result.Stdout != "ok\n" {
		t.Fatalf("resume envelope = %#v", resumed)
	}
}

func TestResumeOmitsUnchangedRunningTask(t *testing.T) {
	fake := newFakeProxyma(t)
	service := NewService(testConfig(t, fake.server.URL))
	if _, err := service.WithStateLock(func() (Envelope, error) {
		return service.Submit(context.Background(), SubmitInput{
			UserGoal: "wait for work", Target: Target{Hostname: "example.test"}, Payload: "hostname",
			Reasoning: "test unchanged polling", HarnessName: "test-agent",
		})
	}); err != nil {
		t.Fatal(err)
	}
	first, err := service.WithStateLock(func() (Envelope, error) { return service.Resume(context.Background()) })
	if err != nil || first.Counts.Changed != 1 {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	second, err := service.WithStateLock(func() (Envelope, error) { return service.Resume(context.Background()) })
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Tasks) != 0 || second.Counts.Checked != 1 || second.Counts.Unchanged != 1 || second.Counts.Changed != 0 {
		t.Fatalf("second=%#v", second)
	}
}

func TestSubmitConstructsEveryPayloadKind(t *testing.T) {
	tests := []struct {
		name    string
		kind    string
		prefix  string
		payload string
	}{
		{name: "posix", kind: "posix_shell", prefix: "ssh {user}@example.test ", payload: "uname -a"},
		{name: "windows", kind: "windows_shell", prefix: "winrs -r:example.test cmd /c ", payload: "ver"},
		{name: "powershell", kind: "powershell", prefix: "winrs -r:example.test powershell -NoProfile -Command ", payload: "Get-Date"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var posted commandRequest
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/health":
					writeTestJSON(w, map[string]any{"status": "ok", "capabilities": []string{idempotencyCapability}})
				case "/v1/agent/targets/resolve":
					writeTestJSON(w, map[string]any{"target": map[string]any{"transport": "test", "os": "test", "hostname": "example.test"}, "canonical_command": map[string]any{"prefix": test.prefix, "payload_kind": test.kind}})
				case "/v1/agent/commands":
					if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
						t.Fatal(err)
					}
					w.WriteHeader(http.StatusAccepted)
					writeTestJSON(w, map[string]any{"task_id": "proxyma-task", "status": "created", "client_operation_id": posted.ClientOperationID})
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			service := NewService(testConfig(t, server.URL))
			envelope, err := service.WithStateLock(func() (Envelope, error) {
				return service.Submit(context.Background(), SubmitInput{UserGoal: "test", Target: Target{Hostname: "example.test"}, Payload: test.payload, Reasoning: "test", HarnessName: "test-agent"})
			})
			if err != nil {
				t.Fatal(err)
			}
			if posted.Command != test.prefix+test.payload || envelope.Task.Operations[0].PayloadKind != test.kind {
				t.Fatalf("posted=%#v task=%#v", posted, envelope.Task)
			}
		})
	}
}

func TestResumeReplaysSubmissionUnknownWithSameOperation(t *testing.T) {
	fake := newFakeProxyma(t)
	cfg := testConfig(t, fake.server.URL)
	service := NewService(cfg)
	now := time.Now().UTC()
	task := TaskState{
		SchemaVersion: StateVersion, ClientTaskID: "81fbef9c-0394-42d7-861e-61aa4f1e0816", Revision: 1, UserGoal: "test", Status: "waiting_proxyma", CreatedAt: now, UpdatedAt: now, RemainingWork: "test",
		Operations: []Operation{{OperationID: "c7b2de27-a00f-4c6c-bb90-b4cc80d1628f", RequestHash: "sha256:31ed93b8b2833c1a1ceafead28b509a4674950950570326b9501c52517c527ce", Phase: "submission_unknown", Target: Target{Hostname: "example.test"}, Payload: "hostname", PayloadKind: "posix_shell", CanonicalCommand: "ssh {user}@example.test hostname", Reasoning: "test", HarnessName: "test-agent", PreparedAt: now}},
	}
	if err := service.state.Save(task); err != nil {
		t.Fatal(err)
	}
	if _, err := service.WithStateLock(func() (Envelope, error) { return service.Resume(context.Background()) }); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.submissions != 1 || fake.lastCommand.ClientOperationID != task.Operations[0].OperationID {
		t.Fatalf("submission count=%d request=%#v", fake.submissions, fake.lastCommand)
	}
}

func TestStateJournalRecoveryAndRevision(t *testing.T) {
	root := t.TempDir()
	state := NewStateStore(root)
	now := time.Now().UTC()
	task := TaskState{SchemaVersion: StateVersion, ClientTaskID: "f152c66c-33e6-4012-982f-4bc27603196c", Revision: 2, UserGoal: "test", Status: "pending", CreatedAt: now, UpdatedAt: now, RemainingWork: "continue", Operations: []Operation{}}
	if err := state.journal.WriteJSON(task.ClientTaskID, task); err != nil {
		t.Fatal(err)
	}
	if err := state.Recover(); err != nil {
		t.Fatal(err)
	}
	loaded, err := state.Load(task.ClientTaskID)
	if err != nil || loaded.Revision != 2 {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
	if paths, _ := state.journal.ListJSONFiles(); len(paths) != 0 {
		t.Fatalf("journal paths=%#v", paths)
	}
}

func TestStateSaveRoutesStatusAndMovesTask(t *testing.T) {
	root := t.TempDir()
	state := NewStateStore(root)
	now := time.Now().UTC()
	task := TaskState{SchemaVersion: StateVersion, ClientTaskID: "2bd6c3df-192f-4275-8af4-58bd3cf1b312", Revision: 1, UserGoal: "test", Status: "pending", CreatedAt: now, UpdatedAt: now, RemainingWork: "test", Operations: []Operation{}}
	if err := state.Save(task); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, activeStateDir, task.ClientTaskID+".json")); err != nil {
		t.Fatal(err)
	}
	task.Revision = 2
	task.Status = "blocked"
	task.BlockedReason = "needs a decision"
	task.RequiredNextAction = "review the result"
	task.UpdatedAt = now.Add(time.Second)
	if err := state.Save(task); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, blockedStateDir, task.ClientTaskID+".json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, activeStateDir, task.ClientTaskID+".json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("active copy still exists: %v", err)
	}
	task.Revision = 3
	task.Status = "completed"
	task.BlockedReason = ""
	task.RequiredNextAction = ""
	task.FinalSummary = "done"
	task.UpdatedAt = now.Add(2 * time.Second)
	if err := state.Save(task); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, closedStateDir, task.ClientTaskID+".json")); err != nil {
		t.Fatal(err)
	}
	if _, err := state.Load(task.ClientTaskID); err != nil {
		t.Fatal(err)
	}
}

func TestResumeIgnoresClosedAndReportsBlocked(t *testing.T) {
	root := t.TempDir()
	state := NewStateStore(root)
	now := time.Now().UTC()
	blocked := TaskState{SchemaVersion: StateVersion, ClientTaskID: "0d6dcf98-2be7-4d4b-a95b-8fb55b5d2bea", Revision: 1, UserGoal: "blocked", Status: "blocked", CreatedAt: now, UpdatedAt: now, BlockedReason: "command failed", RequiredNextAction: "choose whether to retry", Operations: []Operation{}}
	if err := state.Save(blocked); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, closedStateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, closedStateDir, "corrupt.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewService(testConfig(t, "http://127.0.0.1:1"))
	service.state = state
	resumed, err := service.Resume(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Action != "blocked" || len(resumed.Tasks) != 1 || resumed.Tasks[0].ClientTaskID != blocked.ClientTaskID {
		t.Fatalf("resume=%#v", resumed)
	}
}

func TestStateRejectsLegacyFlatLayout(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, legacyStateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, legacyStateDir, "task.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewStateStore(root).List()
	typed, ok := err.(*commandError)
	if !ok || typed.code != "state_layout" {
		t.Fatalf("error=%#v", err)
	}
}

func BenchmarkListActiveIgnoresClosedTasks(b *testing.B) {
	state := NewStateStore(b.TempDir())
	now := time.Now().UTC()
	for i := 0; i < 100; i++ {
		task := TaskState{SchemaVersion: StateVersion, ClientTaskID: uuid.NewString(), Revision: 1, UserGoal: "active", Status: "pending", CreatedAt: now, UpdatedAt: now, RemainingWork: "active", Operations: []Operation{}}
		if err := state.Save(task); err != nil {
			b.Fatal(err)
		}
	}
	for i := 0; i < 5000; i++ {
		task := TaskState{SchemaVersion: StateVersion, ClientTaskID: uuid.NewString(), Revision: 1, UserGoal: "closed", Status: "completed", CreatedAt: now, UpdatedAt: now, FinalSummary: "done", Operations: []Operation{}}
		if err := state.Save(task); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		items, err := state.ListActive()
		if err != nil || len(items) != 100 {
			b.Fatalf("items=%d err=%v", len(items), err)
		}
	}
}

func TestClientJWTClaimsAndCapabilityRequirement(t *testing.T) {
	var tokenString string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenString = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		writeTestJSON(w, map[string]any{"status": "ok", "capabilities": []string{idempotencyCapability}})
	}))
	defer server.Close()
	cfg := testConfig(t, server.URL)
	if err := NewClient(cfg).Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	parsed, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) { return []byte(cfg.SigningKey), nil })
	if err != nil || !parsed.Valid || parsed.Header["kid"] != cfg.AgentID {
		t.Fatalf("token=%#v err=%v", parsed, err)
	}
}

func TestClientRejectsMissingIdempotencyCapability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(w, map[string]any{"status": "ok"})
	}))
	defer server.Close()
	err := NewClient(testConfig(t, server.URL)).Health(context.Background())
	typed, ok := err.(*commandError)
	if !ok || typed.code != "server_capability_missing" {
		t.Fatalf("error=%#v", err)
	}
}

func TestClientRefreshesExpiredTokenOnce(t *testing.T) {
	var tokens []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokens = append(tokens, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if len(tokens) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			writeTestJSON(w, map[string]any{"error": "expired", "code": "token_expired"})
			return
		}
		writeTestJSON(w, map[string]any{"status": "ok", "capabilities": []string{idempotencyCapability}})
	}))
	defer server.Close()
	if err := NewClient(testConfig(t, server.URL)).Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 2 || tokens[0] == tokens[1] {
		t.Fatalf("expected one retry with a fresh token; requests=%d", len(tokens))
	}
}

func TestStateLockContention(t *testing.T) {
	state := NewStateStore(t.TempDir())
	lock, err := state.Lock(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	_, err = state.Lock(75 * time.Millisecond)
	typed, ok := err.(*commandError)
	if !ok || typed.code != "state_locked" {
		t.Fatalf("error=%#v", err)
	}
}

func TestMalformedStateIsReportedWithoutModification(t *testing.T) {
	root := t.TempDir()
	tasks := filepath.Join(root, "active")
	if err := os.MkdirAll(tasks, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(tasks, "bad.json")
	original := []byte(`{"schema_version":"2",`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewStateStore(root).List()
	typed, ok := err.(*commandError)
	if !ok || typed.code != "state_corrupt" {
		t.Fatalf("error=%#v", err)
	}
	current, readErr := os.ReadFile(path)
	if readErr != nil || !bytes.Equal(current, original) {
		t.Fatalf("state changed: %q err=%v", current, readErr)
	}
}

func TestUpdateRejectsStaleRevision(t *testing.T) {
	cfg := testConfig(t, "http://127.0.0.1:1")
	service := NewService(cfg)
	now := time.Now().UTC()
	task := TaskState{SchemaVersion: StateVersion, ClientTaskID: "612794af-e059-45a8-8d68-15822465029a", Revision: 1, UserGoal: "test", Status: "pending", CreatedAt: now, UpdatedAt: now, RemainingWork: "test", Operations: []Operation{}}
	if err := service.state.Save(task); err != nil {
		t.Fatal(err)
	}
	_, err := service.WithStateLock(func() (Envelope, error) {
		return service.Update(UpdateInput{ClientTaskID: task.ClientTaskID, ExpectedRevision: 0, RemainingWork: "changed"})
	})
	typed, ok := err.(*commandError)
	if !ok || typed.code != "state_conflict" {
		t.Fatalf("error=%#v", err)
	}
	loaded, _ := service.state.Load(task.ClientTaskID)
	if loaded.Revision != 1 || loaded.RemainingWork != "test" {
		t.Fatalf("task changed=%#v", loaded)
	}
}

func TestVersionDoesNotRequireConfiguration(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var envelope Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil || !envelope.OK || envelope.StateSchema != StateVersion {
		t.Fatalf("envelope=%#v err=%v", envelope, err)
	}
}

func TestLoadConfigResolvesPathsAndEnvironmentKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "agent-configuration"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent-configuration", "agent.json"), []byte(`{"agent_id":"df9ec8bb-1a22-4181-a7c8-126ad553221d","agent_jwt_signing_key":"file-key"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "proxyma-client.json")
	if err := os.WriteFile(configPath, []byte(`{"schema_version":"2","proxyma_base_url":"https://proxyma.example/","agent_config":"agent-configuration/agent.json","state_dir":"task-state","harness_name":"hermes-nightly"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PROXYMA_AGENT_JWT_SIGNING_KEY", "environment-key")
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SigningKey != "environment-key" || cfg.ProxymaBaseURL != "https://proxyma.example" || cfg.HarnessName != "hermes-nightly" || !filepath.IsAbs(cfg.StateDir) {
		t.Fatalf("config=%#v", cfg)
	}
}

func TestConfigDiscoveryPrecedenceAndUpwardSearch(t *testing.T) {
	root := t.TempDir()
	environmentPath := filepath.Join(root, "environment.json")
	t.Setenv("PROXYMA_HARNESS_CONFIG", environmentPath)
	if got, err := discoverConfig("explicit.json"); err != nil || got != "explicit.json" {
		t.Fatalf("explicit got=%q err=%v", got, err)
	}
	if got, err := discoverConfig(""); err != nil || got != environmentPath {
		t.Fatalf("environment got=%q err=%v", got, err)
	}
	t.Setenv("PROXYMA_HARNESS_CONFIG", "")
	harnessDir := filepath.Join(root, "harness")
	nested := filepath.Join(root, "one", "two")
	if err := os.MkdirAll(harnessDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(harnessDir, "proxyma-client.json")
	if err := os.WriteFile(want, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)
	if got, err := discoverConfig(""); err != nil || filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("upward got=%q want=%q err=%v", got, want, err)
	}
}

func testConfig(t *testing.T, baseURL string) Config {
	t.Helper()
	return Config{SchemaVersion: StateVersion, ProxymaBaseURL: baseURL, StateDir: t.TempDir(), HTTPTimeoutSeconds: 5, LockTimeoutSeconds: 1, AgentID: "agent-1", SigningKey: "test-signing-key"}
}
