package integration_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/abdulsalamcodes/ancra/internal/store"
)

// Reconciliation is a platform-operator capability exposed under /admin/orgs/{orgID}/...
// Tests use the admin secret header and a fixed test org ID.

// ---------------------------------------------------------------------------
// GET /admin/orgs/{orgID}/reconciliation
// ---------------------------------------------------------------------------

func TestListReconciliationRuns_Empty(t *testing.T) {
	env := newTestEnv(t)

	resp := env.do(t, http.MethodGet, "/admin/orgs/"+testOrgID+"/reconciliation", nil, admin())
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)
	runs, ok := out["runs"].([]interface{})
	if !ok || len(runs) != 0 {
		t.Fatalf("expected empty runs list, got %v", out["runs"])
	}
}

// ---------------------------------------------------------------------------
// POST /admin/orgs/{orgID}/reconciliation/trigger
// ---------------------------------------------------------------------------

func TestTriggerReconciliation_DeltaZeroWhenBalanced(t *testing.T) {
	env := newTestEnv(t)

	// Register the test org so the reconciliation service can look it up.
	testOrg := seedTestOrg(t, env)

	resp := env.do(t, http.MethodPost, "/admin/orgs/"+testOrg+"/reconciliation/trigger", nil, admin())
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
	testOrg := seedTestOrg(t, env)

	env.do(t, http.MethodPost, "/admin/orgs/"+testOrg+"/reconciliation/trigger", nil, admin())

	resp := env.do(t, http.MethodGet, "/admin/orgs/"+testOrg+"/reconciliation", nil, admin())
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
	testOrg := seedTestOrg(t, env)

	for range 3 {
		resp := env.do(t, http.MethodPost, "/admin/orgs/"+testOrg+"/reconciliation/trigger", nil, admin())
		mustStatus(t, resp, http.StatusOK)
	}

	resp := env.do(t, http.MethodGet, "/admin/orgs/"+testOrg+"/reconciliation?limit=10", nil, admin())
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)
	runs := out["runs"].([]interface{})
	if len(runs) != 3 {
		t.Fatalf("expected 3 runs, got %d", len(runs))
	}
}

// seedTestOrg registers the fixed test org in the fake store and returns its
// ID string. The reconciliation service resolves orgs by ID; the fake Nomba
// server always returns balance 0, so a sweep on a zero-balance pool is ok.
func seedTestOrg(t *testing.T, env *testEnv) string {
	t.Helper()
	orgID := uuid.MustParse(testOrgID)
	if err := env.stores.orgs.CreateOrg(context.Background(), &store.Organization{
		ID:        orgID,
		Name:      "test org",
		Slug:      "test-org",
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seedTestOrg: %v", err)
	}
	return orgID.String()
}
