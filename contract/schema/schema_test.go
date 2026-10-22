package schema

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

// TestSchemaMatchesUpstream compares the target's schema with the golden captured from upstream on the same engine.
// Run with CONTRACT_UPDATE_SCHEMA=1 against the upstream target to refresh the golden.
func TestSchemaMatchesUpstream(t *testing.T) {
	cfg := h.LoadConfig()
	db := h.TargetDB(t)
	// A request makes sure the target finished creating and migrating the database.
	h.CallAPI(t, "GET", "/health")
	snap, err := Take(db)
	if err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", cfg.DBDriver+".json")
	actual, _ := json.MarshalIndent(snap, "", "  ")
	if os.Getenv("CONTRACT_UPDATE_SCHEMA") == "1" {
		if os.Getenv("CONTRACT_TARGET") != "php" {
			t.Fatal("the schema golden can only be captured from the upstream target")
		}
		if err := os.WriteFile(golden, append(actual, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	expected, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("no golden for %s: %v", cfg.DBDriver, err)
	}
	var want Snapshot
	if err := json.Unmarshal(expected, &want); err != nil {
		t.Fatal(err)
	}
	h.AssertEquals(t, want, *snap)
}
