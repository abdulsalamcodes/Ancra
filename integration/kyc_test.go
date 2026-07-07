package integration_test

import (
	"net/http"
	"testing"
)

// ---------------------------------------------------------------------------
// KYC tier upgrade
// ---------------------------------------------------------------------------

func TestKYCTier_UpgradeFromTier1To2(t *testing.T) {
	env := newTestEnv(t)
	customer := createCustomer(t, env, 1)
	customerID := customer["id"].(string)

	resp := env.do(t, http.MethodPut, "/customers/"+customerID+"/kyc-tier",
		map[string]interface{}{"kyc_tier": 2}, authed())
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)

	if int(out["from_tier"].(float64)) != 1 {
		t.Fatalf("expected from_tier 1, got %v", out["from_tier"])
	}
	if int(out["to_tier"].(float64)) != 2 {
		t.Fatalf("expected to_tier 2, got %v", out["to_tier"])
	}
	if out["customer_id"] != customerID {
		t.Fatalf("expected customer_id %s, got %v", customerID, out["customer_id"])
	}
}

func TestKYCTier_UpgradeFromTier2To3(t *testing.T) {
	env := newTestEnv(t)
	customer := createCustomer(t, env, 2)
	customerID := customer["id"].(string)

	resp := env.do(t, http.MethodPut, "/customers/"+customerID+"/kyc-tier",
		map[string]interface{}{"kyc_tier": 3}, authed())
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)
	if int(out["to_tier"].(float64)) != 3 {
		t.Fatalf("expected to_tier 3, got %v", out["to_tier"])
	}
}

func TestKYCTier_DowngradeRejected(t *testing.T) {
	env := newTestEnv(t)
	customer := createCustomer(t, env, 2)
	customerID := customer["id"].(string)

	resp := env.do(t, http.MethodPut, "/customers/"+customerID+"/kyc-tier",
		map[string]interface{}{"kyc_tier": 1}, authed())
	defer resp.Body.Close()
	mustStatus(t, resp, http.StatusUnprocessableEntity)
}

func TestKYCTier_SameTierRejected(t *testing.T) {
	env := newTestEnv(t)
	customer := createCustomer(t, env, 2)
	customerID := customer["id"].(string)

	resp := env.do(t, http.MethodPut, "/customers/"+customerID+"/kyc-tier",
		map[string]interface{}{"kyc_tier": 2}, authed())
	defer resp.Body.Close()
	mustStatus(t, resp, http.StatusUnprocessableEntity)
}

func TestKYCTier_InvalidTierRejected(t *testing.T) {
	env := newTestEnv(t)
	customer := createCustomer(t, env, 1)
	customerID := customer["id"].(string)

	resp := env.do(t, http.MethodPut, "/customers/"+customerID+"/kyc-tier",
		map[string]interface{}{"kyc_tier": 99}, authed())
	defer resp.Body.Close()
	mustStatus(t, resp, http.StatusBadRequest)
}

// ---------------------------------------------------------------------------
// KYC tier history
// ---------------------------------------------------------------------------

func TestKYCTier_HistoryIsRecorded(t *testing.T) {
	env := newTestEnv(t)
	customer := createCustomer(t, env, 1)
	customerID := customer["id"].(string)

	// Upgrade tier 1 → 2
	env.do(t, http.MethodPut, "/customers/"+customerID+"/kyc-tier",
		map[string]interface{}{"kyc_tier": 2}, authed())

	// Upgrade tier 2 → 3
	env.do(t, http.MethodPut, "/customers/"+customerID+"/kyc-tier",
		map[string]interface{}{"kyc_tier": 3}, authed())

	resp := env.do(t, http.MethodGet, "/customers/"+customerID+"/kyc-tier/history", nil, authed())
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)

	history := out["history"].([]interface{})
	if len(history) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(history))
	}
}

func TestKYCTier_HistoryEmptyForFreshCustomer(t *testing.T) {
	env := newTestEnv(t)
	customer := createCustomer(t, env, 1)
	customerID := customer["id"].(string)

	resp := env.do(t, http.MethodGet, "/customers/"+customerID+"/kyc-tier/history", nil, authed())
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)

	history := out["history"].([]interface{})
	if len(history) != 0 {
		t.Fatalf("expected empty history, got %d entries", len(history))
	}
}
