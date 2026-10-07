package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// triggerRequestTimeout bounds a single HTTP request to the trigger/run
// endpoints. Individual requests are generous; the overall poll loop is
// governed by the caller's context, not this timeout.
const triggerRequestTimeout = 11 * time.Minute

// pollInterval is the delay between run-status polls while awaiting completion.
// It is a var so tests can shorten it.
var pollInterval = 2 * time.Second

// runTimeout bounds how long a single item's run is allowed to poll before
// giving up. It is a var so tests can shorten it.
var runTimeout = 10 * time.Minute

// triggerResponse is the JSON body of a successful /trigger POST.
type triggerResponse struct {
	Success  bool   `json:"success"`
	RunID    string `json:"runId"`
	RunIDAlt string `json:"run_id"`
}

// runsResponse is the JSON body of the /runs poll. The list arrives under
// either "runs" or "data".
type runsResponse struct {
	Runs []runInfo `json:"runs"`
	Data []runInfo `json:"data"`
}

// runInfo is one run entry in the /runs poll. Summary is kept raw so the
// fallback path can marshal it verbatim.
type runInfo struct {
	ID           string          `json:"id"`
	Status       string          `json:"status"`
	ErrorMessage string          `json:"errorMessage"`
	Summary      json.RawMessage `json:"summary"`
}

// sendAgentTrigger runs the agent via the trusted project trigger API and
// returns the final answer text. It posts the prompt, then polls the agent's
// runs until the run reaches a terminal state or the context deadline elapses.
// Transport/protocol failures are returned as errors so one item's failure never
// aborts the whole run.
func sendAgentTrigger(ctx context.Context, base, token, projectID, agentID, question string) (string, error) {
	base = strings.TrimRight(base, "/")
	runID, err := triggerAgent(ctx, base, token, projectID, agentID, question)
	if err != nil {
		return "", err
	}

	// Bound the polling phase so no single status can hang the whole run. Use
	// the caller's deadline when it fires earlier than runTimeout.
	pollCtx := ctx
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > runTimeout {
		var cancel context.CancelFunc
		pollCtx, cancel = context.WithTimeout(ctx, runTimeout)
		defer cancel()
	}
	return awaitRun(pollCtx, base, token, projectID, agentID, runID)
}

// triggerAgent posts the prompt to the trigger endpoint and returns the run id.
func triggerAgent(ctx context.Context, base, token, projectID, agentID, question string) (string, error) {
	payload, err := json.Marshal(map[string]string{"prompt": question})
	if err != nil {
		return "", fmt.Errorf("marshal trigger request: %w", err)
	}

	url := fmt.Sprintf("%s/api/projects/%s/agents/%s/trigger", base, projectID, agentID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("build trigger request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: triggerRequestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("trigger request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read trigger response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("trigger HTTP %d: %s", resp.StatusCode, truncate(strings.TrimSpace(string(body)), 500))
	}

	var tr triggerResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", fmt.Errorf("decode trigger response: %w", err)
	}
	runID := tr.RunID
	if runID == "" {
		runID = tr.RunIDAlt
	}
	if runID == "" {
		return "", fmt.Errorf("trigger response missing runId")
	}
	return runID, nil
}

// awaitRun polls the runs endpoint until the matching run reaches a terminal
// status, returning the final answer (or an error for failed runs / deadlines).
func awaitRun(ctx context.Context, base, token, projectID, agentID, runID string) (string, error) {
	url := fmt.Sprintf("%s/api/projects/%s/agents/%s/runs", base, projectID, agentID)
	client := &http.Client{Timeout: triggerRequestTimeout}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	lastStatus := ""
	for {
		run, found, err := fetchRun(ctx, client, url, token, runID)
		if err != nil {
			return "", err
		}
		if found {
			lastStatus = run.Status
			switch {
			case run.Status == "completed":
				return finalResponse(run)
			case isClarificationStatus(run.Status):
				return "", clarificationError(runID, run.Status)
			case isFailedStatus(run.Status):
				return "", runFailure(runID, run)
			case isInProgressStatus(run.Status):
				// keep polling
			default:
				return "", fmt.Errorf("run %s ended in unrecognised state %q", runID, run.Status)
			}
		}

		select {
		case <-ctx.Done():
			return "", fmt.Errorf("run %s did not reach a terminal state within %s (last status %q)", runID, runTimeout, lastStatus)
		case <-ticker.C:
		}
	}
}

// isClarificationStatus reports whether a status means the agent asked for
// clarification instead of answering.
func isClarificationStatus(s string) bool {
	switch s {
	case "input-required", "input_required", "paused", "awaiting-input", "awaiting_input", "requires-input":
		return true
	default:
		return false
	}
}

// isFailedStatus reports whether a status is a terminal failure.
func isFailedStatus(s string) bool {
	switch s {
	case "failed", "cancelled", "skipped":
		return true
	default:
		return false
	}
}

// isInProgressStatus reports whether a status means the run is still working.
func isInProgressStatus(s string) bool {
	switch s {
	case "working", "pending", "queued", "running", "submitted", "started", "":
		return true
	default:
		return false
	}
}

// clarificationError renders an error for a run that asked for clarification.
func clarificationError(runID, status string) error {
	return fmt.Errorf("run %s ended in state %q (agent requested clarification instead of answering)", runID, status)
}

// fetchRun fetches the runs list and returns the entry matching runID, if found.
func fetchRun(ctx context.Context, client *http.Client, url, token, runID string) (runInfo, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return runInfo{}, false, fmt.Errorf("build runs request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return runInfo{}, false, fmt.Errorf("runs request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return runInfo{}, false, fmt.Errorf("read runs response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return runInfo{}, false, fmt.Errorf("runs HTTP %d: %s", resp.StatusCode, truncate(strings.TrimSpace(string(body)), 500))
	}

	var rr runsResponse
	if err := json.Unmarshal(body, &rr); err != nil {
		return runInfo{}, false, fmt.Errorf("decode runs response: %w", err)
	}
	runs := rr.Runs
	if len(runs) == 0 {
		runs = rr.Data
	}
	for _, r := range runs {
		if r.ID == runID {
			return r, true, nil
		}
	}
	return runInfo{}, false, nil
}

// finalResponse returns summary.final_response for a completed run, falling back
// to the raw summary JSON when final_response is empty.
func finalResponse(run runInfo) (string, error) {
	var sum struct {
		FinalResponse string `json:"final_response"`
	}
	if len(run.Summary) > 0 {
		if err := json.Unmarshal(run.Summary, &sum); err == nil && sum.FinalResponse != "" {
			return sum.FinalResponse, nil
		}
		if s := strings.TrimSpace(string(run.Summary)); s != "" {
			return s, nil
		}
	}
	return "", fmt.Errorf("run completed with empty response")
}

// runFailure renders an error for a run that ended in a non-completed state.
func runFailure(runID string, run runInfo) error {
	msg := strings.TrimSpace(run.ErrorMessage)
	if msg == "" {
		msg = "no error message"
	}
	return fmt.Errorf("agent run %s: %s: %s", runID, run.Status, msg)
}

// truncate limits a string to n characters for error messages.
func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
