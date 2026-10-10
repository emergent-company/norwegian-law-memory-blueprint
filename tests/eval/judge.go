package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// JudgeConfig configures the optional LLM answer-quality judge.
type JudgeConfig struct {
	BaseURL      string
	Model        string
	APIKey       string
	ExtraHeaders map[string]string
}

// JudgeResult is the judge's output for one item.
type JudgeResult struct {
	PointRecall float64 `json:"point_recall"`
	Faithful    bool    `json:"faithful"`
	Reason      string  `json:"reason"`
}

const judgeSystemPrompt = "" +
	"You are a strict legal-answer evaluator. Given a question, a gold set of " +
	"answer points, and a candidate answer, count how many of the gold points are " +
	"covered by the candidate answer (a point is covered only if its substance is " +
	"present, not merely alluded to) and whether the candidate answer is faithful " +
	"to the provisions it cites (no fabricated rules, no overstatement of what a " +
	"provision says). " +
	"Respond with a single JSON object and nothing else, in the exact shape: " +
	"{\"covered\": <int>, \"total\": <int>, \"faithful\": <bool>, \"reason\": \"<short reason>\"}"

// judgeFromEnv builds a JudgeConfig from JUDGE_BASE_URL / JUDGE_MODEL /
// JUDGE_API_KEY / JUDGE_EXTRA_HEADERS. Returns ok=false when the judge is not
// configured (base URL or model missing).
func judgeFromEnv() (JudgeConfig, bool) {
	base := os.Getenv("JUDGE_BASE_URL")
	model := os.Getenv("JUDGE_MODEL")
	if base == "" || model == "" {
		return JudgeConfig{}, false
	}
	cfg := JudgeConfig{BaseURL: base, Model: model, APIKey: os.Getenv("JUDGE_API_KEY")}
	if raw := os.Getenv("JUDGE_EXTRA_HEADERS"); raw != "" {
		var m map[string]string
		if err := json.Unmarshal([]byte(raw), &m); err == nil {
			cfg.ExtraHeaders = m
		}
	}
	return cfg, true
}

// runJudge posts an OpenAI-compatible /chat/completions request and parses the
// JSON verdict.
func runJudge(ctx context.Context, cfg JudgeConfig, question, answer string, goldPoints []string) (JudgeResult, error) {
	userMsg := judgeUserMessage(question, answer, goldPoints)
	body := map[string]any{
		"model": cfg.Model,
		"messages": []map[string]any{
			{"role": "system", "content": judgeSystemPrompt},
			{"role": "user", "content": userMsg},
		},
		"temperature": 0,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return JudgeResult{}, fmt.Errorf("marshal judge request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return JudgeResult{}, fmt.Errorf("build judge request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	for k, v := range cfg.ExtraHeaders {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 3 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return JudgeResult{}, fmt.Errorf("judge request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return JudgeResult{}, fmt.Errorf("read judge response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return JudgeResult{}, fmt.Errorf("judge HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var cr struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &cr); err != nil {
		return JudgeResult{}, fmt.Errorf("decode judge response: %w", err)
	}
	if len(cr.Choices) == 0 || cr.Choices[0].Message.Content == "" {
		return JudgeResult{}, fmt.Errorf("judge returned empty content")
	}
	return parseJudgeContent(cr.Choices[0].Message.Content, len(goldPoints))
}

// judgeUserMessage renders the question, gold points and candidate answer for the
// judge model.
func judgeUserMessage(question, answer string, goldPoints []string) string {
	gp, _ := json.Marshal(goldPoints)
	return fmt.Sprintf("QUESTION:\n%s\n\nGOLD POINTS (JSON array):\n%s\n\nCANDIDATE ANSWER:\n%s",
		question, string(gp), answer)
}

// parseJudgeContent extracts the judge's JSON verdict from model output,
// tolerating markdown code fences.
func parseJudgeContent(content string, fallbackTotal int) (JudgeResult, error) {
	s := stripCodeFences(content)
	var parsed struct {
		Covered  *int   `json:"covered"`
		Total    *int   `json:"total"`
		Faithful *bool  `json:"faithful"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(s), &parsed); err != nil {
		return JudgeResult{}, fmt.Errorf("parse judge JSON %q: %w", s, err)
	}

	res := JudgeResult{
		Faithful: parsed.Faithful != nil && *parsed.Faithful,
		Reason:   parsed.Reason,
	}
	denom := fallbackTotal
	if parsed.Total != nil && *parsed.Total > 0 {
		denom = *parsed.Total
	}
	if denom <= 0 {
		return res, nil
	}
	covered := 0
	if parsed.Covered != nil {
		covered = *parsed.Covered
	}
	if covered < 0 {
		covered = 0
	}
	res.PointRecall = float64(covered) / float64(denom)
	if res.PointRecall > 1 {
		res.PointRecall = 1
	}
	return res, nil
}

// stripCodeFences removes markdown ```json ... ``` fences from model output.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
