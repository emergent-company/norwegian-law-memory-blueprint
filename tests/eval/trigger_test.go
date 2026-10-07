package eval

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fastPoll shortens the run-poll interval so polling tests complete quickly.
func fastPoll(t *testing.T) {
	t.Helper()
	old := pollInterval
	pollInterval = 10 * time.Millisecond
	t.Cleanup(func() { pollInterval = old })
}

// setRunTimeout overrides the run timeout and poll interval for one test,
// restoring both afterwards.
func setRunTimeout(t *testing.T, timeout, interval time.Duration) {
	t.Helper()
	oldTimeout, oldInterval := runTimeout, pollInterval
	runTimeout, pollInterval = timeout, interval
	t.Cleanup(func() { runTimeout, pollInterval = oldTimeout, oldInterval })
}

func TestSendAgentTriggerHappyPath(t *testing.T) {
	fastPoll(t)

	var runsPolls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/projects/p1/agents/a1/trigger":
			writeJSON(w, http.StatusOK, `{"success":true,"runId":"run-123"}`)
		case "/api/projects/p1/agents/a1/runs":
			runsPolls++
			if runsPolls == 1 {
				writeJSON(w, http.StatusOK, `{"runs":[{"id":"run-123","status":"working","summary":{}}]}`)
				return
			}
			writeJSON(w, http.StatusOK, `{"runs":[{"id":"run-123","status":"completed","summary":{"final_response":"FULL ANSWER"}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	got, err := sendAgentTrigger(context.Background(), srv.URL, "tok", "p1", "a1", "question")
	if err != nil {
		t.Fatalf("sendAgentTrigger: %v", err)
	}
	if got != "FULL ANSWER" {
		t.Errorf("answer = %q, want %q", got, "FULL ANSWER")
	}
	if runsPolls != 2 {
		t.Errorf("runs polled %d times, want 2", runsPolls)
	}
}

func TestSendAgentTriggerFailure(t *testing.T) {
	fastPoll(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/projects/p1/agents/a1/trigger":
			writeJSON(w, http.StatusOK, `{"success":true,"runId":"run-9"}`)
		case "/api/projects/p1/agents/a1/runs":
			writeJSON(w, http.StatusOK, `{"runs":[{"id":"run-9","status":"failed","errorMessage":"tool crashed"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	_, err := sendAgentTrigger(context.Background(), srv.URL, "tok", "p1", "a1", "q")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "tool crashed") {
		t.Errorf("error = %q, want it to contain %q", err, "tool crashed")
	}
}

func TestSendAgentTriggerNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusUnauthorized, `{"error":"bad token"}`)
	}))
	defer srv.Close()

	_, err := sendAgentTrigger(context.Background(), srv.URL, "tok", "p1", "a1", "q")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error = %q, want it to mention status 401", err)
	}
}

func TestSendAgentTriggerInputRequired(t *testing.T) {
	fastPoll(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/projects/p1/agents/a1/trigger":
			writeJSON(w, http.StatusOK, `{"success":true,"runId":"run-7"}`)
		case "/api/projects/p1/agents/a1/runs":
			writeJSON(w, http.StatusOK, `{"runs":[{"id":"run-7","status":"input-required","summary":{}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	_, err := sendAgentTrigger(context.Background(), srv.URL, "tok", "p1", "a1", "q")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "input-required") {
		t.Errorf("error = %q, want it to contain %q", err, "input-required")
	}
	if !strings.Contains(err.Error(), "clarification") {
		t.Errorf("error = %q, want it to contain %q", err, "clarification")
	}
}

func TestSendAgentTriggerTimeout(t *testing.T) {
	setRunTimeout(t, 150*time.Millisecond, 20*time.Millisecond)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/projects/p1/agents/a1/trigger":
			writeJSON(w, http.StatusOK, `{"success":true,"runId":"run-5"}`)
		case "/api/projects/p1/agents/a1/runs":
			writeJSON(w, http.StatusOK, `{"runs":[{"id":"run-5","status":"working","summary":{}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	_, err := sendAgentTrigger(context.Background(), srv.URL, "tok", "p1", "a1", "q")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "did not reach a terminal state") {
		t.Errorf("error = %q, want it to contain %q", err, "did not reach a terminal state")
	}
}

// writeJSON writes a JSON body with the given status code.
func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
