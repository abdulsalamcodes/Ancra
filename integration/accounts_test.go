package integration_test

import (
	"net/http"
	"testing"
)

// ---------------------------------------------------------------------------
// Provisioning
// ---------------------------------------------------------------------------

func TestCreateAccount_Valid(t *testing.T) {
	env := newTestEnv(t)
	customer := createCustomer(t, env, 1)
	customerID := customer["id"].(string)

	resp := env.do(t, http.MethodPost, "/accounts", map[string]interface{}{
		"customer_id":    customerID,
		"display_name":   "Jane Doe",
		"customer_email": "jane@example.com",
	}, authed())
	mustStatus(t, resp, http.StatusCreated)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)

	acct := out["account"].(map[string]interface{})
	if acct["bank_account_number"] == "" {
		t.Fatal("expected a bank_account_number from fake Nomba")
	}
	if acct["status"] != "active" {
		t.Fatalf("expected status active, got %v", acct["status"])
	}
}

func TestCreateAccount_MissingRequiredFields(t *testing.T) {
	env := newTestEnv(t)
	customer := createCustomer(t, env, 1)

	// Missing display_name and customer_email
	resp := env.do(t, http.MethodPost, "/accounts", map[string]interface{}{
		"customer_id": customer["id"].(string),
	}, authed())
	defer resp.Body.Close()
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestCreateAccount_InvalidCustomerID(t *testing.T) {
	env := newTestEnv(t)

	resp := env.do(t, http.MethodPost, "/accounts", map[string]interface{}{
		"customer_id":    "not-a-uuid",
		"display_name":   "Jane",
		"customer_email": "jane@example.com",
	}, authed())
	defer resp.Body.Close()
	mustStatus(t, resp, http.StatusBadRequest)
}

func TestCreateAccount_NonexistentCustomer(t *testing.T) {
	env := newTestEnv(t)

	resp := env.do(t, http.MethodPost, "/accounts", map[string]interface{}{
		"customer_id":    "00000000-0000-0000-0000-000000000000",
		"display_name":   "Ghost",
		"customer_email": "ghost@example.com",
	}, authed())
	defer resp.Body.Close()
	mustStatus(t, resp, http.StatusInternalServerError)
}

func TestCreateAccount_Idempotent(t *testing.T) {
	env := newTestEnv(t)
	customer := createCustomer(t, env, 1)
	customerID := customer["id"].(string)

	first := createAccount(t, env, customerID)
	second := createAccount(t, env, customerID)

	firstID := first["account"].(map[string]interface{})["id"]
	secondID := second["account"].(map[string]interface{})["id"]

	if firstID != secondID {
		t.Fatalf("idempotency broken: got two different account IDs: %v vs %v", firstID, secondID)
	}
}

// ---------------------------------------------------------------------------
// Get account
// ---------------------------------------------------------------------------

func TestGetAccount_Found(t *testing.T) {
	env := newTestEnv(t)
	customer := createCustomer(t, env, 1)
	created := createAccount(t, env, customer["id"].(string))
	accountID := created["account"].(map[string]interface{})["id"].(string)

	resp := env.do(t, http.MethodGet, "/accounts/"+accountID, nil, authed())
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)
	if out["id"] != accountID {
		t.Fatalf("expected account id %s, got %v", accountID, out["id"])
	}
}

func TestGetAccount_NotFound(t *testing.T) {
	env := newTestEnv(t)

	resp := env.do(t, http.MethodGet, "/accounts/00000000-0000-0000-0000-000000000001", nil, authed())
	defer resp.Body.Close()
	mustStatus(t, resp, http.StatusNotFound)
}

func TestGetAccount_InvalidUUID(t *testing.T) {
	env := newTestEnv(t)

	resp := env.do(t, http.MethodGet, "/accounts/not-a-uuid", nil, authed())
	defer resp.Body.Close()
	mustStatus(t, resp, http.StatusBadRequest)
}

// ---------------------------------------------------------------------------
// Balance
// ---------------------------------------------------------------------------

func TestGetBalance_ZeroOnFreshAccount(t *testing.T) {
	env := newTestEnv(t)
	customer := createCustomer(t, env, 1)
	created := createAccount(t, env, customer["id"].(string))
	accountID := created["account"].(map[string]interface{})["id"].(string)

	resp := env.do(t, http.MethodGet, "/accounts/"+accountID+"/balance", nil, authed())
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)
	if out["balance"] != float64(0) {
		t.Fatalf("expected balance 0, got %v", out["balance"])
	}
	if out["currency"] != "NGN" {
		t.Fatalf("expected currency NGN, got %v", out["currency"])
	}
}

// ---------------------------------------------------------------------------
// Transactions & Statement
// ---------------------------------------------------------------------------

func TestListTransactions_EmptyOnFreshAccount(t *testing.T) {
	env := newTestEnv(t)
	customer := createCustomer(t, env, 1)
	created := createAccount(t, env, customer["id"].(string))
	accountID := created["account"].(map[string]interface{})["id"].(string)

	resp := env.do(t, http.MethodGet, "/accounts/"+accountID+"/transactions", nil, authed())
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)
	entries, ok := out["entries"].([]interface{})
	if !ok || entries == nil {
		// nil is fine — no entries yet
		return
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestGetStatement_EmptyOnFreshAccount(t *testing.T) {
	env := newTestEnv(t)
	customer := createCustomer(t, env, 1)
	created := createAccount(t, env, customer["id"].(string))
	accountID := created["account"].(map[string]interface{})["id"].(string)

	resp := env.do(t, http.MethodGet, "/accounts/"+accountID+"/statement", nil, authed())
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)
	if out["opening_balance_kobo"] != float64(0) {
		t.Fatalf("expected opening balance 0, got %v", out["opening_balance_kobo"])
	}
	if out["closing_balance_kobo"] != float64(0) {
		t.Fatalf("expected closing balance 0, got %v", out["closing_balance_kobo"])
	}
}

func TestGetStatement_AfterCredit(t *testing.T) {
	env := newTestEnv(t)
	customer := createCustomer(t, env, 1)
	created := createAccount(t, env, customer["id"].(string))
	acct := created["account"].(map[string]interface{})
	accountID := acct["id"].(string)
	bankAccountNumber := acct["bank_account_number"].(string)

	// Simulate an inbound credit via webhook
	body, sig := webhookBody(t, "txn-stmt-001", bankAccountNumber, 500.00)
	wResp := postWebhook(t, env, body, sig)
	mustStatus(t, wResp, http.StatusOK)

	resp := env.do(t, http.MethodGet, "/accounts/"+accountID+"/statement", nil, authed())
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)

	// 500 NGN = 50000 kobo
	if out["closing_balance_kobo"] != float64(50000) {
		t.Fatalf("expected closing balance 50000 kobo, got %v", out["closing_balance_kobo"])
	}
	entries := out["entries"].([]interface{})
	if len(entries) != 1 {
		t.Fatalf("expected 1 statement entry, got %d", len(entries))
	}
}

// ---------------------------------------------------------------------------
// Rename (PUT /accounts/{id})
// ---------------------------------------------------------------------------

func TestUpdateAccount_Rename(t *testing.T) {
	env := newTestEnv(t)
	customer := createCustomer(t, env, 1)
	created := createAccount(t, env, customer["id"].(string))
	accountID := created["account"].(map[string]interface{})["id"].(string)

	resp := env.do(t, http.MethodPut, "/accounts/"+accountID,
		map[string]string{"display_name": "Jane Smith"}, authed())
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)
	if out["status"] != "updated" {
		t.Fatalf("expected status updated, got %v", out["status"])
	}
}

func TestUpdateAccount_MissingDisplayName(t *testing.T) {
	env := newTestEnv(t)
	customer := createCustomer(t, env, 1)
	created := createAccount(t, env, customer["id"].(string))
	accountID := created["account"].(map[string]interface{})["id"].(string)

	resp := env.do(t, http.MethodPut, "/accounts/"+accountID,
		map[string]string{}, authed())
	defer resp.Body.Close()
	mustStatus(t, resp, http.StatusBadRequest)
}

// ---------------------------------------------------------------------------
// Close (POST /accounts/{id}/close)
// ---------------------------------------------------------------------------

func TestCloseAccount(t *testing.T) {
	env := newTestEnv(t)
	customer := createCustomer(t, env, 1)
	created := createAccount(t, env, customer["id"].(string))
	accountID := created["account"].(map[string]interface{})["id"].(string)

	resp := env.do(t, http.MethodPost, "/accounts/"+accountID+"/close", nil, authed())
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)
	if out["status"] != "closed" {
		t.Fatalf("expected status closed, got %v", out["status"])
	}

	// Verify status is closed via GET
	get := env.do(t, http.MethodGet, "/accounts/"+accountID, nil, authed())
	mustStatus(t, get, http.StatusOK)
	var acct map[string]interface{}
	decodeJSON(t, get, &acct)
	if acct["status"] != "closed" {
		t.Fatalf("expected account status closed after close, got %v", acct["status"])
	}
}

func TestCloseAccount_AlreadyClosed(t *testing.T) {
	env := newTestEnv(t)
	customer := createCustomer(t, env, 1)
	created := createAccount(t, env, customer["id"].(string))
	accountID := created["account"].(map[string]interface{})["id"].(string)

	// Close once
	env.do(t, http.MethodPost, "/accounts/"+accountID+"/close", nil, authed())

	// Close again — should fail
	resp := env.do(t, http.MethodPost, "/accounts/"+accountID+"/close", nil, authed())
	defer resp.Body.Close()
	mustStatus(t, resp, http.StatusInternalServerError)
}
