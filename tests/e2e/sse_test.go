package e2e

import (
	"strings"
	"testing"
)

// TestParseSSE exercises parseAgentResponse (the pure JSON/SSE response decoding
// + text extraction) entirely offline — no server or env vars are required.
func TestParseSSE(t *testing.T) {
	t.Run("completed_with_artifact", func(t *testing.T) {
		stream := strings.Join([]string{
			`data: {"task":{"id":"t1","contextId":"c1","status":{"state":"TASK_STATE_SUBMITTED"}}}`,
			``,
			`data: {"statusUpdate":{"taskId":"t1","contextId":"c1","status":{"state":"TASK_STATE_WORKING"}}}`,
			``,
			`data: {"artifactUpdate":{"taskId":"t1","contextId":"c1","artifact":{"artifactId":"result","parts":[{"text":"Her er "}]}}}`,
			``,
			`data: {"task":{"id":"t1","contextId":"c1","status":{"state":"TASK_STATE_COMPLETED"},"artifacts":[{"artifactId":"result","parts":[{"text":"Her er svaret."}]}]}}`,
			``,
		}, "\n")

		answer, status, err := parseAgentResponse([]byte(stream), "text/event-stream")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if status != "TASK_STATE_COMPLETED" {
			t.Errorf("status = %q, want TASK_STATE_COMPLETED", status)
		}
		if answer != "Her er svaret." {
			t.Errorf("answer = %q, want %q", answer, "Her er svaret.")
		}
	})

	t.Run("failed_state", func(t *testing.T) {
		stream := strings.Join([]string{
			`data: {"task":{"id":"t1","contextId":"c1","status":{"state":"TASK_STATE_SUBMITTED"}}}`,
			``,
			`data: {"statusUpdate":{"taskId":"t1","contextId":"c1","status":{"state":"TASK_STATE_FAILED","message":{"messageId":"m1","role":"ROLE_AGENT","parts":[{"text":"boom"}]}}}}`,
			``,
		}, "\n")

		_, status, err := parseAgentResponse([]byte(stream), "text/event-stream")
		if err == nil {
			t.Fatal("expected error for failed state, got nil")
		}
		if status != "TASK_STATE_FAILED" {
			t.Errorf("status = %q, want TASK_STATE_FAILED", status)
		}
		if !strings.Contains(err.Error(), "TASK_STATE_FAILED") || !strings.Contains(err.Error(), "boom") {
			t.Errorf("error %q should mention TASK_STATE_FAILED and boom", err.Error())
		}
	})

	t.Run("json_envelope", func(t *testing.T) {
		body := `{"task":{"id":"t1","contextId":"c1","status":{"state":"TASK_STATE_COMPLETED"},"artifacts":[{"artifactId":"result","parts":[{"text":"JSON svar."}]}]}}`

		answer, status, err := parseAgentResponse([]byte(body), "application/a2a+json")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if status != "TASK_STATE_COMPLETED" {
			t.Errorf("status = %q, want TASK_STATE_COMPLETED", status)
		}
		if answer != "JSON svar." {
			t.Errorf("answer = %q, want %q", answer, "JSON svar.")
		}
	})

	t.Run("json_body_sniffed_without_content_type", func(t *testing.T) {
		// A JSON bare-message body with an empty content type must still be
		// detected via body sniffing.
		body := `{"message":{"messageId":"m1","role":"ROLE_AGENT","parts":[{"text":"sniffed"}]}}`

		answer, status, err := parseAgentResponse([]byte(body), "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if answer != "sniffed" {
			t.Errorf("answer = %q, want %q", answer, "sniffed")
		}
		if status != "" {
			t.Errorf("status = %q, want empty (bare message)", status)
		}
	})
}
