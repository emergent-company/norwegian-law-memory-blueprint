package e2e

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	sdkacp "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/acp"
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

// runAgent runs a single synchronous ACP run against the given agent and returns
// the concatenated text of all output parts. It fails the test on a transport
// error, a non-completed status, a non-nil run error, or a human-in-the-loop
// AwaitRequest.
func runAgent(t *testing.T, base, token, question string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	// A real agent run (model + tool calls) can take minutes, so the SDK's
	// default 30s HTTP timeout is far too short.
	client := sdkacp.NewClientWithHTTP(base, token, &http.Client{Timeout: 11 * time.Minute})
	run, err := client.CreateRun(ctx, "norwegian-law-assistant", sdkacp.CreateRunRequest{
		Message: []sdkacp.MessagePart{{ContentType: "text/plain", Content: question}},
		Mode:    "sync",
	})
	if err != nil {
		t.Fatalf("CreateRun failed: %v", err)
	}
	if run.AwaitRequest != nil {
		t.Fatalf("agent asked for clarification instead of answering (%q): %q", question, run.AwaitRequest.Question)
	}
	if run.Error != nil {
		t.Fatalf("run error: %s: %s", run.Error.Code, run.Error.Message)
	}
	if run.Status != "completed" {
		t.Fatalf("expected status completed, got %q", run.Status)
	}

	var sb strings.Builder
	for _, m := range run.Output {
		for _, p := range m.Parts {
			sb.WriteString(p.Content)
		}
	}
	return sb.String()
}
