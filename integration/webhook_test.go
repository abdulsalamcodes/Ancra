package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// ---------------------------------------------------------------------------
// Signature verification
// ---------------------------------------------------------------------------

func TestWebhook_MissingSignature(t *testing.T) {
	env := newTestEnv(t)

	body, _ := webhookBody(t, "txn-nosig", "1234567890", 100)
	req, _ := http.NewRequest(http.MethodPost, env.server.URL+"/webhooks/nomba", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// No X-Nomba-Signature header

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestWebhook_InvalidSignature(t *testing.T) {
	env := newTestEnv(t)

	body, _ := webhookBody(t, "txn-badsig", "1234567890", 100)
	resp := postWebhook(t, env, body, "deadbeef")
	defer resp.Body.Close()
	mustStatus(t, resp, http.StatusUnauthorized)
}

// ---------------------------------------------------------------------------
// Credit flow
// ---------------------------------------------------------------------------

func TestWebhook_CreditSuccess(t *testing.T) {
	env := newTestEnv(t)

	customer := createCustomer(t, env, 1)
	created := createAccount(t, env, customer["id"].(string))
	acct := created["account"].(map[string]interface{})
	accountID := acct["id"].(string)
	bankAccountNumber := acct["bank_account_number"].(string)

	// Send a 1000 NGN credit webhook
	body, sig := webhookBody(t, "txn-credit-001", bankAccountNumber, 1000.00)
	resp := postWebhook(t, env, body, sig)
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)
	if out["status"] != "processed" {
		t.Fatalf("expected status processed, got %v", out["status"])
	}

	// Balance should now be 100000 kobo (1000 NGN × 100)
	balResp := env.do(t, http.MethodGet, "/accounts/"+accountID+"/balance", nil, authed())
	mustStatus(t, balResp, http.StatusOK)
	var balOut map[string]interface{}
	decodeJSON(t, balResp, &balOut)
	if balOut["balance"] != float64(100000) {
		t.Fatalf("expected balance 100000 kobo, got %v", balOut["balance"])
	}
}

func TestWebhook_CreditUpdatesTransactions(t *testing.T) {
	env := newTestEnv(t)

	customer := createCustomer(t, env, 1)
	created := createAccount(t, env, customer["id"].(string))
	acct := created["account"].(map[string]interface{})
	accountID := acct["id"].(string)
	bankAccountNumber := acct["bank_account_number"].(string)

	body, sig := webhookBody(t, "txn-credit-002", bankAccountNumber, 250.00)
	postWebhook(t, env, body, sig)

	txResp := env.do(t, http.MethodGet, "/accounts/"+accountID+"/transactions", nil, authed())
	mustStatus(t, txResp, http.StatusOK)
	var txOut map[string]interface{}
	decodeJSON(t, txResp, &txOut)

	entries := txOut["entries"].([]interface{})
	if len(entries) != 1 {
		t.Fatalf("expected 1 transaction entry, got %d", len(entries))
	}
	entry := entries[0].(map[string]interface{})
	if entry["direction"] != "credit" {
		t.Fatalf("expected credit direction, got %v", entry["direction"])
	}
	if entry["amount"] != float64(25000) { // 250 NGN → 25000 kobo
		t.Fatalf("expected amount 25000 kobo, got %v", entry["amount"])
	}
}

// ---------------------------------------------------------------------------
// Idempotency — exactly-once crediting
// ---------------------------------------------------------------------------

func TestWebhook_DuplicateCreditIsNoop(t *testing.T) {
	env := newTestEnv(t)

	customer := createCustomer(t, env, 1)
	created := createAccount(t, env, customer["id"].(string))
	acct := created["account"].(map[string]interface{})
	accountID := acct["id"].(string)
	bankAccountNumber := acct["bank_account_number"].(string)

	// Deliver the same webhook twice
	body, sig := webhookBody(t, "txn-dedup-001", bankAccountNumber, 500.00)

	resp1 := postWebhook(t, env, body, sig)
	mustStatus(t, resp1, http.StatusOK)
	var out1 map[string]interface{}
	decodeJSON(t, resp1, &out1)
	if out1["status"] != "processed" {
		t.Fatalf("first delivery: expected processed, got %v", out1["status"])
	}

	resp2 := postWebhook(t, env, body, sig)
	mustStatus(t, resp2, http.StatusOK)
	var out2 map[string]interface{}
	decodeJSON(t, resp2, &out2)
	if out2["status"] != "duplicate" {
		t.Fatalf("second delivery: expected duplicate, got %v", out2["status"])
	}

	// Balance must reflect exactly one credit (500 NGN = 50000 kobo)
	balResp := env.do(t, http.MethodGet, "/accounts/"+accountID+"/balance", nil, authed())
	mustStatus(t, balResp, http.StatusOK)
	var balOut map[string]interface{}
	decodeJSON(t, balResp, &balOut)
	if balOut["balance"] != float64(50000) {
		t.Fatalf("expected balance 50000 kobo after dedup, got %v", balOut["balance"])
	}
}

// ---------------------------------------------------------------------------
// Suspense — unknown account number
// ---------------------------------------------------------------------------

func TestWebhook_UnknownAccount_GoesToSuspense(t *testing.T) {
	env := newTestEnv(t)

	body, sig := webhookBody(t, "txn-suspense-001", "9999999999", 200.00)
	resp := postWebhook(t, env, body, sig)
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)
	if out["status"] != "suspense" {
		t.Fatalf("expected status suspense, got %v", out["status"])
	}

	// Suspense system account should have a positive balance
	suspenseAcct, err := env.stores.ledger.GetSystemAccount(context.Background(), "suspense")
	if err != nil {
		t.Fatalf("get suspense account: %v", err)
	}
	balance, err := env.stores.ledger.GetBalance(context.Background(), suspenseAcct.ID)
	if err != nil {
		t.Fatalf("get suspense balance: %v", err)
	}
	if balance != 20000 { // 200 NGN → 20000 kobo
		t.Fatalf("expected suspense balance 20000 kobo, got %d", balance)
	}
}

// ---------------------------------------------------------------------------
// Unhandled event type
// ---------------------------------------------------------------------------

func TestWebhook_UnhandledEvent_Ignored(t *testing.T) {
	env := newTestEnv(t)

	payload := map[string]interface{}{
		"event":     "some.other.event",
		"requestId": "req-001",
		"transaction": map[string]interface{}{
			"transactionId": "txn-other-001",
			"accountNumber": "1234567890",
			"amount":        100.0,
			"currency":      "NGN",
		},
		"customer": map[string]interface{}{},
		"merchant": map[string]interface{}{},
	}
	b, _ := json.Marshal(payload)
	sig := signBody(b, testWebhookSecret)

	resp := postWebhook(t, env, b, sig)
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)
	if out["status"] != "ignored" {
		t.Fatalf("expected status ignored, got %v", out["status"])
	}
}

// ---------------------------------------------------------------------------
// Multiple credits accumulate correctly
// ---------------------------------------------------------------------------

func TestWebhook_MultipleCreditsAccumulate(t *testing.T) {
	env := newTestEnv(t)

	customer := createCustomer(t, env, 1)
	created := createAccount(t, env, customer["id"].(string))
	acct := created["account"].(map[string]interface{})
	accountID := acct["id"].(string)
	bankAccountNumber := acct["bank_account_number"].(string)

	credits := []struct {
		txnID  string
		amount float64
	}{
		{"txn-multi-001", 100.00},
		{"txn-multi-002", 200.00},
		{"txn-multi-003", 300.00},
	}

	for _, c := range credits {
		body, sig := webhookBody(t, c.txnID, bankAccountNumber, c.amount)
		resp := postWebhook(t, env, body, sig)
		mustStatus(t, resp, http.StatusOK)
	}

	// Expected: (100 + 200 + 300) NGN = 60000 kobo
	balResp := env.do(t, http.MethodGet, "/accounts/"+accountID+"/balance", nil, authed())
	mustStatus(t, balResp, http.StatusOK)
	var balOut map[string]interface{}
	decodeJSON(t, balResp, &balOut)
	if balOut["balance"] != float64(60000) {
		t.Fatalf("expected accumulated balance 60000 kobo, got %v", balOut["balance"])
	}
}

// ---------------------------------------------------------------------------
// Outbound webhook delivery is enqueued after credit
// ---------------------------------------------------------------------------

func TestWebhook_CreditEnqueuesOutboundDelivery(t *testing.T) {
	env := newTestEnv(t)

	customer := createCustomer(t, env, 1)
	created := createAccount(t, env, customer["id"].(string))
	acct := created["account"].(map[string]interface{})
	bankAccountNumber := acct["bank_account_number"].(string)

	body, sig := webhookBody(t, "txn-delivery-001", bankAccountNumber, 150.00)
	postWebhook(t, env, body, sig)

	resp := env.do(t, http.MethodGet, "/admin/webhooks", nil, admin())
	mustStatus(t, resp, http.StatusOK)
	var out map[string]interface{}
	decodeJSON(t, resp, &out)

	deliveries := out["deliveries"].([]interface{})
	if len(deliveries) != 1 {
		t.Fatalf("expected 1 webhook delivery enqueued, got %d", len(deliveries))
	}
	d := deliveries[0].(map[string]interface{})
	if d["event_type"] != "collection.credit.success" {
		t.Fatalf("expected event_type collection.credit.success, got %v", d["event_type"])
	}
	if d["status"] != "pending" {
		t.Fatalf("expected status pending, got %v", d["status"])
	}
}

// Silence unused import of io (used only by postWebhook helper in setup).
var _ io.Reader
