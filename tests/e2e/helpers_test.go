package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
// the test on a transport error, a non-2xx response, a non-completed task state,
// a failed/cancelled task, or a human-in-the-loop (input-required) pause.
//
// The /message:send endpoint returns a JSON envelope on repo HEAD but the
// deployed dev build streams text/event-stream, so we POST directly and tolerate
// both wire formats (see parseAgentResponse).
func runAgent(t *testing.T, base, token, question string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	reqBody := a2a.SendMessageRequest{
		Message: a2a.Message{
			MessageID: fmt.Sprintf("e2e-%d", time.Now().UnixNano()),
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{a2a.TextPart(question)},
			Metadata:  map[string]any{"skillId": agentSkillID},
		},
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/message:send", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", a2a.A2AContentType)
	req.Header.Set("Accept", "application/json, text/event-stream")

	// A real agent run (model + tool calls) can take minutes, so no short read
	// deadline. The whole response is buffered, which is fine within the timeout.
	client := &http.Client{Timeout: 11 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode >= 400 {
		t.Fatalf("SendMessage HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	answer, _, err := parseAgentResponse(respBody, resp.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	return answer
}

// parseAgentResponse parses a /message:send response body and returns the
// assistant's answer text, the terminal task state, and any error. It tolerates
// both wire formats:
//   - JSON: a buffered a2a.SendMessageResponse envelope (repo HEAD behaviour).
//   - SSE: text/event-stream frames carrying a2a.StreamResponse events (deployed
//     dev behaviour).
//
// contentType may be empty; when it does not disambiguate, the format is sniffed
// from the body (leading '{' => JSON, leading 'data:' => SSE).
func parseAgentResponse(body []byte, contentType string) (answer string, status string, err error) {
	switch detectResponseFormat(contentType, body) {
	case "json":
		var resp a2a.SendMessageResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return "", "", fmt.Errorf("decode JSON message:send response: %w", err)
		}
		if resp.Task != nil {
			return taskAnswer(resp.Task)
		}
		if resp.Message != nil {
			return extractMessageText(resp.Message), "", nil
		}
		return "", "", fmt.Errorf("message:send returned neither a task nor a message")
	case "sse":
		return parseSSEResponse(body)
	default:
		return "", "", fmt.Errorf("unrecognised message:send response (content-type %q, body starts %q)", contentType, firstChars(body, 20))
	}
}

// detectResponseFormat classifies a /message:send response body as "json", "sse",
// or "unknown" using the content type and a body sniff. Body sniffing takes
// precedence because a streaming proxy may not echo the content type faithfully.
func detectResponseFormat(contentType string, body []byte) string {
	ct := strings.ToLower(contentType)
	trimmed := strings.TrimSpace(string(body))
	switch {
	case strings.HasPrefix(trimmed, "{"):
		return "json"
	case strings.HasPrefix(trimmed, "data:"):
		return "sse"
	case strings.Contains(ct, "json"):
		return "json"
	case strings.Contains(ct, "event-stream"):
		return "sse"
	default:
		return "unknown"
	}
}

// firstChars returns the first n characters of the body, for error messages.
func firstChars(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) > n {
		return s[:n]
	}
	return s
}

// parseSSEResponse parses an SSE body of a2a.StreamResponse frames, tracking the
// latest task (with status updates applied) and the latest bare message. It
// returns the answer from the terminal task (or the bare message).
func parseSSEResponse(body []byte) (string, string, error) {
	var latestTask *a2a.Task
	var latestMessage *a2a.Message

	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue // skip event:/id:/retry:/comment/blank lines
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var ev a2a.StreamResponse
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			return "", "", fmt.Errorf("decode SSE event: %w", err)
		}
		if ev.Task != nil {
			latestTask = ev.Task
		}
		if ev.Message != nil {
			latestMessage = ev.Message
		}
		if ev.StatusUpdate != nil {
			if latestTask != nil && latestTask.ID == ev.StatusUpdate.TaskID {
				latestTask.Status = ev.StatusUpdate.Status
			} else if latestTask == nil {
				latestTask = &a2a.Task{
					ID:        ev.StatusUpdate.TaskID,
					ContextID: ev.StatusUpdate.ContextID,
					Status:    ev.StatusUpdate.Status,
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return "", "", fmt.Errorf("read SSE stream: %w", err)
	}

	if latestTask != nil {
		return taskAnswer(latestTask)
	}
	if latestMessage != nil {
		return extractMessageText(latestMessage), "", nil
	}
	return "", "", fmt.Errorf("SSE stream contained no task or message")
}

// taskAnswer validates a task's terminal state and returns the final assistant
// text plus the state. It errors on failed/cancelled/input-required states and
// any other non-completed state. The synchronous A2A path returns a Task whose
// first artifact ("result") carries the final assistant text; we fall back to
// agent-authored history when the artifacts are empty.
func taskAnswer(task *a2a.Task) (string, string, error) {
	status := string(task.Status.State)
	switch task.Status.State {
	case a2a.TaskStateCompleted:
		// success
	case a2a.TaskStateInputRequired:
		return "", status, fmt.Errorf("agent asked for clarification instead of answering: %s", taskStatusText(task))
	case a2a.TaskStateFailed, a2a.TaskStateCanceled:
		return "", status, fmt.Errorf("task ended in state %s: %s", task.Status.State, taskStatusText(task))
	default:
		return "", status, fmt.Errorf("expected task state %s, got %s", a2a.TaskStateCompleted, task.Status.State)
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
		return sb.String(), status, nil
	}
	// Fall back to history (agent-authored text parts).
	for _, m := range task.History {
		for _, p := range m.Parts {
			if p.Text != nil {
				sb.WriteString(*p.Text)
			}
		}
	}
	return sb.String(), status, nil
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
