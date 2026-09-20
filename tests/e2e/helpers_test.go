package e2e

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/a2a"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/auth"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

// envFirst returns the first non-empty environment variable among names.
func envFirst(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return ""
}

// serverURL returns TEST_SERVER_URL, falling back to MEMORY_SERVER_URL.
func serverURL() string {
	return envFirst("TEST_SERVER_URL", "MEMORY_SERVER_URL")
}

// apiToken returns TEST_API_TOKEN, falling back to MEMORY_PROJECT_API_KEY.
func apiToken() string {
	return envFirst("TEST_API_TOKEN", "MEMORY_PROJECT_API_KEY")
}

// requireCreds resolves the server URL and API token, skipping the test if
// either is empty so the suite is safe to run without credentials.
func requireCreds(t *testing.T) (string, string) {
	t.Helper()
	base := serverURL()
	token := apiToken()
	if base == "" || token == "" {
		t.Skip("set TEST_SERVER_URL and TEST_API_TOKEN (or MEMORY_SERVER_URL / MEMORY_PROJECT_API_KEY)")
	}
	return base, token
}

// newGraphClient builds a graph client for the given server URL and project API
// token. The emt_* token embeds the project ID, so org/project context is empty.
func newGraphClient(t *testing.T, base, token string) *graph.Client {
	t.Helper()
	httpClient := &http.Client{Timeout: 5 * time.Minute}
	return graph.NewClient(httpClient, base, auth.NewAPITokenProvider(token), "", "")
}

// blueprintDir resolves the blueprint directory: LAW_BLUEPRINT_DIR if set,
// otherwise the repository root (derived from the test source file location).
func blueprintDir() string {
	if d := os.Getenv("LAW_BLUEPRINT_DIR"); d != "" {
		return d
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	// file is <root>/tests/e2e/<file>; three Dir() calls reach <root>.
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

// agentSkillID is the A2A skill id that routes a message to the
// norwegian-law-assistant. A2A's SendMessageRequest has no skill selector, so
// the target agent is chosen via message.metadata["skillId"], which the server
// resolves as the agent definition's RFC 1123 slug (acpslug.FromName of the
// agent name). See apps/server/domain/agents/a2a_message.go resolveA2AAgent.
const agentSkillID = "norwegian-law-assistant"

// runAgent sends a single synchronous A2A message to the norwegian-law-assistant
// skill and returns the concatenated text of the assistant's answer. It fails
// the test on a transport error, a non-completed task state, a failed/cancelled
// task, or a human-in-the-loop (input-required) pause.
func runAgent(t *testing.T, base, token, question string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	// A real agent run (model + tool calls) can take minutes, so the SDK's
	// default 30s HTTP timeout is far too short.
	client := a2a.NewClientWithHTTP(base, token, &http.Client{Timeout: 11 * time.Minute})
	resp, err := client.SendMessage(ctx, a2a.SendMessageRequest{
		Message: a2a.Message{
			MessageID: fmt.Sprintf("e2e-%d", time.Now().UnixNano()),
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{a2a.TextPart(question)},
			Metadata:  map[string]any{"skillId": agentSkillID},
		},
	})
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	if resp.Task != nil {
		return extractTaskAnswer(t, resp.Task, question)
	}
	if resp.Message != nil {
		return extractMessageText(resp.Message)
	}
	t.Fatalf("SendMessage returned neither a task nor a message")
	return ""
}

// extractTaskAnswer validates the task's terminal state and returns the final
// assistant text. The synchronous A2A path returns a Task whose first artifact
// ("result") carries the final assistant text; we fall back to history when the
// artifacts are empty.
func extractTaskAnswer(t *testing.T, task *a2a.Task, question string) string {
	t.Helper()
	switch task.Status.State {
	case a2a.TaskStateCompleted:
		// success
	case a2a.TaskStateInputRequired:
		t.Fatalf("agent asked for clarification instead of answering (%q): %s", question, taskStatusText(task))
	case a2a.TaskStateFailed, a2a.TaskStateCanceled:
		t.Fatalf("task ended in state %s: %s", task.Status.State, taskStatusText(task))
	default:
		t.Fatalf("expected task state %s, got %s", a2a.TaskStateCompleted, task.Status.State)
	}

	var sb strings.Builder
	for _, art := range task.Artifacts {
		for _, p := range art.Parts {
			if p.Text != nil {
				sb.WriteString(*p.Text)
			}
		}
	}
	if sb.Len() > 0 {
		return sb.String()
	}
	// Fall back to history (agent-authored text parts).
	for _, m := range task.History {
		for _, p := range m.Parts {
			if p.Text != nil {
				sb.WriteString(*p.Text)
			}
		}
	}
	return sb.String()
}

// taskStatusText returns the human-readable text of a task's status message, or
// the bare state when no message is present.
func taskStatusText(task *a2a.Task) string {
	if task.Status.Message != nil {
		if s := extractMessageText(task.Status.Message); s != "" {
			return s
		}
	}
	return string(task.Status.State)
}

// extractMessageText concatenates the text parts of a bare A2A message.
func extractMessageText(m *a2a.Message) string {
	var sb strings.Builder
	for _, p := range m.Parts {
		if p.Text != nil {
			sb.WriteString(*p.Text)
		}
	}
	return sb.String()
}
