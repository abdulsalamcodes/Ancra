package integration_test

import (
	"bytes"
	"net/http"
	"testing"
)

func TestCreateCustomer_Valid(t *testing.T) {
	env := newTestEnv(t)

	resp := env.do(t, http.MethodPost, "/customers",
		map[string]interface{}{"kyc_tier": 1}, authed())
	mustStatus(t, resp, http.StatusCreated)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)

	if out["id"] == "" {
		t.Fatal("expected non-empty id")
	}
	if out["kyc_tier"] != float64(1) {
		t.Fatalf("expected kyc_tier 1, got %v", out["kyc_tier"])
	}
}

func TestCreateCustomer_DefaultKYCTier(t *testing.T) {
	env := newTestEnv(t)

	// Omitting kyc_tier should default to 1.
	resp := env.do(t, http.MethodPost, "/customers",
		map[string]interface{}{}, authed())
	mustStatus(t, resp, http.StatusCreated)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)
	if out["kyc_tier"] != float64(1) {
		t.Fatalf("expected default kyc_tier 1, got %v", out["kyc_tier"])
	}
}

func TestCreateCustomer_InvalidKYCTier(t *testing.T) {
	env := newTestEnv(t)

	resp := env.do(t, http.MethodPost, "/customers",
		map[string]interface{}{"kyc_tier": 5}, authed())
	defer resp.Body.Close()
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestCreateCustomer_InvalidJSON(t *testing.T) {
	env := newTestEnv(t)

	req, err := http.NewRequest(http.MethodPost, env.server.URL+"/customers",
		bytes.NewReader([]byte(`{bad json`)))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testStaticKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestListCustomers_Empty(t *testing.T) {
	env := newTestEnv(t)

	resp := env.do(t, http.MethodGet, "/customers", nil, authed())
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)

	customers, ok := out["customers"].([]interface{})
	if !ok {
		t.Fatalf("expected customers array, got %T", out["customers"])
	}
	if len(customers) != 0 {
		t.Fatalf("expected 0 customers, got %d", len(customers))
	}
}

func TestListCustomers_AfterCreation(t *testing.T) {
	env := newTestEnv(t)

	createCustomer(t, env, 1)
	createCustomer(t, env, 2)

	resp := env.do(t, http.MethodGet, "/customers", nil, authed())
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)

	customers := out["customers"].([]interface{})
	if len(customers) != 2 {
		t.Fatalf("expected 2 customers, got %d", len(customers))
	}
}

func TestListCustomers_Pagination(t *testing.T) {
	env := newTestEnv(t)

	for i := 0; i < 5; i++ {
		createCustomer(t, env, 1)
	}

	resp := env.do(t, http.MethodGet, "/customers?limit=2&offset=0", nil, authed())
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)

	customers := out["customers"].([]interface{})
	if len(customers) != 2 {
		t.Fatalf("expected 2 customers with limit=2, got %d", len(customers))
	}
}
