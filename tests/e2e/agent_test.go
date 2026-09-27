package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAgentBasicQueries drives the norwegian-law-assistant through a set of
// synchronous A2A messages and asserts on the returned answers.
func TestAgentBasicQueries(t *testing.T) {
	base, token := requireCreds(t)

	cases := []struct {
		name     string
		question string
		check    func(t *testing.T, answer string)
	}{
		{
			name:     "what_is_arbeidsmiljoloven",
			question: "Svar kort: hva er arbeidsmiljøloven?",
			check: func(t *testing.T, answer string) {
				if strings.TrimSpace(answer) == "" {
					t.Error("expected at least one non-empty output part")
				}
				t.Logf("answer: %s", answer)
			},
		},
		{
			name:     "arbeidsmiljoloven_ref_id",
			question: "Hvilket Lovdata ref_id har arbeidsmiljøloven? Svar bare med ref_id.",
			check: func(t *testing.T, answer string) {
				// arbeidsmiljøloven = lov/2005-06-17-62 (tvisteloven is -90).
				// Match the date part so the legacy (LOV-) and ref_id (lov/) forms both pass.
				if !strings.Contains(strings.ToLower(answer), "2005-06-17-62") {
					t.Errorf("answer does not contain arbeidsmiljøloven ref_id 2005-06-17-62: %q", answer)
				}
			},
		},
		{
			name:     "avtaleloven_36",
			question: "Hva sier avtaleloven § 36? Svar kort på norsk.",
			check: func(t *testing.T, answer string) {
				lower := strings.ToLower(answer)
				if !strings.Contains(lower, "avtaleloven") {
					t.Errorf("answer does not mention avtaleloven: %q", answer)
				}
				if !strings.Contains(lower, "36") {
					t.Errorf("answer does not mention §36: %q", answer)
				}
			},
		},
		{
			name:     "oppsigelse_paragraphs",
			question: "Hvilke paragrafer har arbeidsmiljøloven om oppsigelse?",
			check: func(t *testing.T, answer string) {
				if strings.TrimSpace(answer) == "" {
					t.Error("expected non-empty answer")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			answer := runAgent(t, base, token, tc.question)
			tc.check(t, answer)
		})
	}
}

// TestAgentIsReadOnly is fully offline: it parses the agent's tool grants and
// asserts the agent has no write tools and no wildcards. It must pass in CI
// without any credentials.
func TestAgentIsReadOnly(t *testing.T) {
	tools := parseToolsList(t)
	if len(tools) == 0 {
		t.Fatal("agent tools list is empty")
	}

	allow := map[string]bool{
		"search-hybrid":         true,
		"search-semantic":       true,
		"search-knowledge":      true,
		"entity-query":          true,
		"entity-search":         true,
		"entity-edges-get":      true,
		"entity-history":        true,
		"entity-type-list":      true,
		"relationship-list":     true,
		"graph-traverse":        true,
		"tag-list":              true,
		"schema-list":           true,
		"schema-get":            true,
		"schema-list-installed": true,
		"schema-compiled-types": true,
		"document-list":         true,
		"document-get":          true,
		"project-briefing":      true,
		"ask_user":              true,
	}

	for _, tool := range tools {
		if tool == "*" {
			t.Errorf("tool list contains wildcard entry %q", tool)
		}
		if strings.Contains(tool, "*") {
			t.Errorf("tool entry %q contains a wildcard", tool)
		}
		if !allow[tool] {
			t.Errorf("tool %q is not in the read-only allowlist", tool)
		}
	}
}

// parseToolsList reads the tools block from agents/norwegian-law-assistant.yaml
// without an external YAML dependency, using a simple line scanner.
func parseToolsList(t *testing.T) []string {
	t.Helper()
	path := filepath.Join(blueprintDir(), "agents", "norwegian-law-assistant.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read agent yaml: %v", err)
	}

	var tools []string
	inTools := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "tools:" {
			inTools = true
			continue
		}
		if !inTools {
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// A top-level key (no leading whitespace) ends the tools block.
		if line == trimmed {
			break
		}
		// Only "- value" list items count as tool entries.
		if !strings.HasPrefix(trimmed, "- ") && trimmed != "-" {
			break
		}
		val := stripQuotes(strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
		if val != "" {
			tools = append(tools, val)
		}
	}
	return tools
}

// stripQuotes removes surrounding single or double quotes from a value.
func stripQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
