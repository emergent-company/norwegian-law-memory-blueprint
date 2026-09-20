package e2e

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

// TestBlueprintInstall applies the blueprint to a project via the memory CLI.
// It is mutating and opt-in only: LAW_E2E_INSTALL=1 plus TEST_PROJECT_ID.
func TestBlueprintInstall(t *testing.T) {
	if os.Getenv("LAW_E2E_INSTALL") != "1" {
		t.Skip("set LAW_E2E_INSTALL=1 to run blueprint install (mutating)")
	}
	projectID := os.Getenv("TEST_PROJECT_ID")
	if projectID == "" {
		t.Skip("set TEST_PROJECT_ID to run blueprint install")
	}
	base := serverURL()
	token := apiToken()
	if base == "" || token == "" {
		t.Skip("set TEST_SERVER_URL and TEST_API_TOKEN")
	}

	cli := os.Getenv("MEMORY_CLI")
	if cli == "" {
		cli = "memory"
	}
	dir := blueprintDir()

	cmd := exec.Command(cli, "blueprints", "install", dir, "--project", projectID, "--server", base)
	cmd.Env = append(os.Environ(), "CI=1", "MEMORY_PROJECT_API_KEY="+token)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("blueprint install failed: %v\n%s", err, out)
	}
	t.Logf("blueprint install output:\n%s", out)
}

// TestDatasetObjectsLoaded verifies lower-bound object counts per type. The
// bounds are deliberately below the seeded corpus so the assertions stay robust
// to future corpus growth; failures report the actual counts.
func TestDatasetObjectsLoaded(t *testing.T) {
	base, token := requireCreds(t)
	c := newGraphClient(t, base, token)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cases := []struct {
		typ string
		min int
	}{
		{"Law", 700},
		{"Regulation", 5000},
		{"LegalParagraph", 90000},
		{"Ministry", 15},
		{"LegalArea", 200},
	}
	for _, tc := range cases {
		n, err := c.CountObjects(ctx, &graph.CountObjectsOptions{Type: tc.typ})
		if err != nil {
			t.Errorf("CountObjects(%s): %v", tc.typ, err)
			continue
		}
		if n < tc.min {
			t.Errorf("CountObjects(%s) = %d, want >= %d", tc.typ, n, tc.min)
		}
	}
}

// TestKnownLawObjectByKey asserts the canonical Kong Christian Den Femtis Norske
// Lov object is present with the expected type and populated core properties.
func TestKnownLawObjectByKey(t *testing.T) {
	base, token := requireCreds(t)
	c := newGraphClient(t, base, token)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	resp, err := c.ListObjects(ctx, &graph.ListObjectsOptions{Key: "lov/1687-04-15"})
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected exactly 1 item for key lov/1687-04-15, got %d", len(resp.Items))
	}

	obj := resp.Items[0]
	if obj.Type != "Law" {
		t.Errorf("expected Type Law, got %q", obj.Type)
	}
	if refID, _ := obj.Properties["ref_id"].(string); refID == "" {
		t.Errorf("expected non-empty Properties[\"ref_id\"], got %q", refID)
	}
	if title, _ := obj.Properties["title"].(string); title == "" {
		t.Errorf("expected non-empty Properties[\"title\"], got %q", title)
	}
}

// TestRelationshipsLoaded verifies the HAS_PARAGRAPH and ADMINISTERED_BY
// relationship types are populated.
func TestRelationshipsLoaded(t *testing.T) {
	base, token := requireCreds(t)
	c := newGraphClient(t, base, token)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	for _, typ := range []string{"HAS_PARAGRAPH", "ADMINISTERED_BY"} {
		resp, err := c.ListRelationships(ctx, &graph.ListRelationshipsOptions{Type: typ, Limit: 1})
		if err != nil {
			t.Errorf("ListRelationships(%s): %v", typ, err)
			continue
		}
		if resp.Total <= 0 {
			t.Errorf("ListRelationships(%s) Total = %d, want > 0", typ, resp.Total)
		}
	}
}

// TestTraverseLawToParagraphs fetches a Law by key, then walks its
// HAS_PARAGRAPH relationships using the stable entity ID.
func TestTraverseLawToParagraphs(t *testing.T) {
	base, token := requireCreds(t)
	c := newGraphClient(t, base, token)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	resp, err := c.ListObjects(ctx, &graph.ListObjectsOptions{Type: "Law", Key: "lov/1687-04-15", Limit: 1})
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	if len(resp.Items) == 0 {
		t.Fatal("no Law object for key lov/1687-04-15")
	}
	entityID := resp.Items[0].EntityID

	rels, err := c.ListRelationships(ctx, &graph.ListRelationshipsOptions{ObjectID: entityID, Type: "HAS_PARAGRAPH", Limit: 1})
	if err != nil {
		t.Fatalf("ListRelationships: %v", err)
	}
	if rels.Total <= 0 {
		t.Errorf("HAS_PARAGRAPH from %s Total = %d, want > 0", entityID, rels.Total)
	}
}

// TestLexicalSearch verifies full-text search surfaces the arbeidsmiljøloven
// corpus.
func TestLexicalSearch(t *testing.T) {
	base, token := requireCreds(t)
	c := newGraphClient(t, base, token)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	resp, err := c.FTSSearch(ctx, &graph.FTSSearchOptions{Query: "arbeidsmiljøloven", Limit: 10})
	if err != nil {
		t.Fatalf("FTSSearch: %v", err)
	}
	if resp.Total <= 0 && len(resp.Data) == 0 {
		t.Errorf("FTSSearch for %q returned no results", "arbeidsmiljøloven")
	}
}
