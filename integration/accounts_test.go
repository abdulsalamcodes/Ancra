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
	acct := createAccount(t, env, customer["id"].(string))

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

	if first["id"] != second["id"] {
		t.Fatalf("idempotency broken: two different account IDs: %v vs %v", first["id"], second["id"])
	}
}

// ---------------------------------------------------------------------------
// Get account
// ---------------------------------------------------------------------------

func TestGetAccount_Found(t *testing.T) {
	env := newTestEnv(t)
	customer := createCustomer(t, env, 1)
	acct := createAccount(t, env, customer["id"].(string))
	accountID := acct["id"].(string)

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
	acct := createAccount(t, env, customer["id"].(string))
	accountID := acct["id"].(string)

	resp := env.do(t, http.MethodGet, "/accounts/"+accountID+"/balance", nil, authed())
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)
	// AccountBalance has no json tags → "Balance", "Currency"
	if out["Balance"] != float64(0) {
		t.Fatalf("expected Balance 0, got %v", out["Balance"])
	}
	if out["Currency"] != "NGN" {
		t.Fatalf("expected Currency NGN, got %v", out["Currency"])
	}
}

// ---------------------------------------------------------------------------
// Transactions & Statement
// ---------------------------------------------------------------------------

func TestListTransactions_EmptyOnFreshAccount(t *testing.T) {
	env := newTestEnv(t)
	customer := createCustomer(t, env, 1)
	acct := createAccount(t, env, customer["id"].(string))
	accountID := acct["id"].(string)

	resp := env.do(t, http.MethodGet, "/accounts/"+accountID+"/transactions", nil, authed())
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)
	// TransactionPage has no json tags → "Entries"
	entries, ok := out["Entries"].([]interface{})
	if ok && len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestGetStatement_EmptyOnFreshAccount(t *testing.T) {
	env := newTestEnv(t)
	customer := createCustomer(t, env, 1)
	acct := createAccount(t, env, customer["id"].(string))
	accountID := acct["id"].(string)

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
	acct := createAccount(t, env, customer["id"].(string))
	accountID := acct["id"].(string)
	bankAccountNumber := acct["bank_account_number"].(string)

	// Simulate an inbound credit via webhook
	body, sig, ts := webhookBody(t, "txn-stmt-001", bankAccountNumber, 500.00)
	wResp := postWebhook(t, env, body, sig, ts)
	mustStatus(t, wResp, http.StatusOK)

	resp := env.do(t, http.MethodGet, "/accounts/"+accountID+"/statement", nil, authed())
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)

	// 500 NGN = 50000 kobo
	if out["closing_balance_kobo"] != float64(50000) {
		t.Fatalf("expected closing balance 50000 kobo, got %v", out["closing_balance_kobo"])
	}
	// StatementPage has json tags → "entries"
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
	acct := createAccount(t, env, customer["id"].(string))
	accountID := acct["id"].(string)

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
	acct := createAccount(t, env, customer["id"].(string))
	accountID := acct["id"].(string)

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
	acct := createAccount(t, env, customer["id"].(string))
	accountID := acct["id"].(string)

	resp := env.do(t, http.MethodPost, "/accounts/"+accountID+"/close", nil, authed())
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)
	if out["status"] != "closed" {
		t.Fatalf("expected status closed, got %v", out["status"])
	}

	// Verify the account reports closed status via GET
	get := env.do(t, http.MethodGet, "/accounts/"+accountID, nil, authed())
	mustStatus(t, get, http.StatusOK)
	var fetched map[string]interface{}
	decodeJSON(t, get, &fetched)
	if fetched["status"] != "closed" {
		t.Fatalf("expected account status closed after close, got %v", fetched["status"])
	}
}

func TestCloseAccount_AlreadyClosed(t *testing.T) {
	env := newTestEnv(t)
	customer := createCustomer(t, env, 1)
	acct := createAccount(t, env, customer["id"].(string))
	accountID := acct["id"].(string)

	// Close once
	env.do(t, http.MethodPost, "/accounts/"+accountID+"/close", nil, authed())

	// Close again — should fail
	resp := env.do(t, http.MethodPost, "/accounts/"+accountID+"/close", nil, authed())
	defer resp.Body.Close()
	mustStatus(t, resp, http.StatusInternalServerError)
}
