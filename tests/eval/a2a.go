package eval

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/a2a"
)

// agentSkillID routes a message to the norwegian-law-assistant skill via
// message.metadata["skillId"] (see the e2e helpers for the same convention).
const agentSkillID = "norwegian-law-assistant"

// sendAgentMessage posts a single synchronous A2A message to the agent and
// returns the assistant's answer text. It tolerates both JSON and SSE response
// bodies. Transport/protocol failures are returned as errors so one item's
// failure never aborts the whole run.
func sendAgentMessage(ctx context.Context, base, token, question string) (string, error) {
	reqBody := a2a.SendMessageRequest{
		Message: a2a.Message{
			MessageID: fmt.Sprintf("eval-%d", time.Now().UnixNano()),
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{a2a.TextPart(question)},
			Metadata:  map[string]any{"skillId": agentSkillID},
		},
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/message:send", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", a2a.A2AContentType)
	req.Header.Set("Accept", "application/json, text/event-stream")

	client := &http.Client{Timeout: 11 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("send message: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("message:send HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	return parseAgentResponse(respBody, resp.Header.Get("Content-Type"))
}

// parseAgentResponse parses a /message:send response body and returns the answer
// text. JSON and SSE formats are both tolerated; the format is sniffed from the
// content type and body.
func parseAgentResponse(body []byte, contentType string) (string, error) {
	switch detectResponseFormat(contentType, body) {
	case "json":
		var resp a2a.SendMessageResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return "", fmt.Errorf("decode JSON message:send response: %w", err)
		}
		if resp.Task != nil {
			return taskAnswer(resp.Task)
		}
		if resp.Message != nil {
			return extractMessageText(resp.Message), nil
		}
		return "", fmt.Errorf("message:send returned neither a task nor a message")
	case "sse":
		return parseSSEResponse(body)
	default:
		return "", fmt.Errorf("unrecognised message:send response (content-type %q, body starts %q)", contentType, firstChars(body, 20))
	}
}

// detectResponseFormat classifies a response body as "json", "sse", or "unknown".
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

// firstChars returns the first n characters of b for error messages.
func firstChars(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) > n {
		return s[:n]
	}
	return s
}

// parseSSEResponse parses an SSE body of a2a.StreamResponse frames and returns
// the answer from the terminal task (or the latest bare message).
func parseSSEResponse(body []byte) (string, error) {
	var latestTask *a2a.Task
	var latestMessage *a2a.Message

	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var ev a2a.StreamResponse
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			return "", fmt.Errorf("decode SSE event: %w", err)
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
		if ev.ArtifactUpdate != nil {
			au := ev.ArtifactUpdate
			if latestTask == nil || latestTask.ID != au.TaskID {
				latestTask = &a2a.Task{ID: au.TaskID, ContextID: au.ContextID}
			}
			latestTask.Artifacts = mergeArtifact(latestTask.Artifacts, au.Artifact, au.Append)
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("read SSE stream: %w", err)
	}

	if latestTask != nil {
		return taskAnswer(latestTask)
	}
	if latestMessage != nil {
		return extractMessageText(latestMessage), nil
	}
	return "", fmt.Errorf("SSE stream contained no task or message")
}

// mergeArtifact merges an artifact-update delta into a task's artifact list.
func mergeArtifact(artifacts []a2a.Artifact, delta a2a.Artifact, appendMode bool) []a2a.Artifact {
	for i := range artifacts {
		if delta.ArtifactID != "" && artifacts[i].ArtifactID == delta.ArtifactID {
			if appendMode {
				artifacts[i].Parts = append(artifacts[i].Parts, delta.Parts...)
			} else {
				artifacts[i] = delta
			}
			return artifacts
		}
	}
	return append(artifacts, delta)
}

// taskAnswer validates a task's terminal state and returns the final assistant
// text. It errors on failed/cancelled/input-required/non-completed states.
func taskAnswer(task *a2a.Task) (string, error) {
	switch task.Status.State {
	case a2a.TaskStateCompleted:
		// success
	case a2a.TaskStateInputRequired:
		return "", fmt.Errorf("agent asked for clarification instead of answering: %s", taskStatusText(task))
	case a2a.TaskStateFailed, a2a.TaskStateCanceled:
		return "", fmt.Errorf("task ended in state %s: %s", task.Status.State, taskStatusText(task))
	default:
		return "", fmt.Errorf("expected task state %s, got %s", a2a.TaskStateCompleted, task.Status.State)
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
		return sb.String(), nil
	}
	// Fall back to agent-authored history only — the user prompt must not leak.
	for _, m := range task.History {
		if m.Role != a2a.RoleAgent {
			continue
		}
		for _, p := range m.Parts {
			if p.Text != nil {
				sb.WriteString(*p.Text)
			}
		}
	}
	return sb.String(), nil
}

// taskStatusText returns the human-readable text of a task's status message.
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
