package integration_test

import (
	"net/http"
	"testing"
)

// ---------------------------------------------------------------------------
// GET /reconciliation
// ---------------------------------------------------------------------------

func TestListReconciliationRuns_Empty(t *testing.T) {
	env := newTestEnv(t)

	resp := env.do(t, http.MethodGet, "/reconciliation", nil, authed())
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)
	runs, ok := out["runs"].([]interface{})
	if !ok || len(runs) != 0 {
		t.Fatalf("expected empty runs list, got %v", out["runs"])
	}
}

// ---------------------------------------------------------------------------
// POST /reconciliation/trigger
// ---------------------------------------------------------------------------

func TestTriggerReconciliation_DeltaZeroWhenBalanced(t *testing.T) {
	env := newTestEnv(t)

	// Fake Nomba server returns availableFloat=0.0, and our pool ledger is also 0.
	resp := env.do(t, http.MethodPost, "/reconciliation/trigger", nil, authed())
	mustStatus(t, resp, http.StatusOK)

	var run map[string]interface{}
	decodeJSON(t, resp, &run)

	if run["status"] != "ok" {
		t.Fatalf("expected reconciliation status ok, got %v", run["status"])
	}
	if run["delta"] != float64(0) {
		t.Fatalf("expected delta 0, got %v", run["delta"])
	}
}

func TestTriggerReconciliation_AppendedToList(t *testing.T) {
	env := newTestEnv(t)

	// Trigger a run
	env.do(t, http.MethodPost, "/reconciliation/trigger", nil, authed())

	// Now list should have 1 run
	resp := env.do(t, http.MethodGet, "/reconciliation", nil, authed())
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)
	runs := out["runs"].([]interface{})
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
}

func TestTriggerReconciliation_MultipleRuns(t *testing.T) {
	env := newTestEnv(t)

	for i := 0; i < 3; i++ {
		resp := env.do(t, http.MethodPost, "/reconciliation/trigger", nil, authed())
		mustStatus(t, resp, http.StatusOK)
	}

	resp := env.do(t, http.MethodGet, "/reconciliation?limit=10", nil, authed())
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)
	runs := out["runs"].([]interface{})
	if len(runs) != 3 {
		t.Fatalf("expected 3 runs, got %d", len(runs))
	}
}
