package integration_test

import (
	"net/http"
	"testing"
	"time"
)

// TestStatement_RunningBalanceIsCorrect verifies that the running_balance_kobo
// on each statement entry is computed correctly across multiple credits.
//
// Spec: "GET /accounts/{id}/statement — opening balance, ordered entries,
// running balance, closing balance"
func TestStatement_RunningBalanceIsCorrect(t *testing.T) {
	env := newTestEnv(t)

	customer := createCustomer(t, env, 1)
	acct := createAccount(t, env, customer["id"].(string))
	accountID := acct["id"].(string)
	bankAccountNumber := acct["bank_account_number"].(string)

	// Post 3 credits with small sleeps to guarantee distinct timestamps.
	credits := []struct {
		txnID  string
		naira  float64
		kobo   int64
	}{
		{"txn-stmt-run-001", 100.00, 10000},
		{"txn-stmt-run-002", 200.00, 20000},
		{"txn-stmt-run-003", 300.00, 30000},
	}

	for _, c := range credits {
		body, sig, ts := webhookBody(t, c.txnID, bankAccountNumber, c.naira)
		postWebhook(t, env, body, sig, ts)
		time.Sleep(2 * time.Millisecond) // ensure distinct CreatedAt
	}

	resp := env.do(t, http.MethodGet, "/accounts/"+accountID+"/statement", nil, authed())
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)

	// Totals: 100 + 200 + 300 = 600 NGN = 60000 kobo
	if out["closing_balance_kobo"] != float64(60000) {
		t.Fatalf("expected closing_balance_kobo 60000, got %v", out["closing_balance_kobo"])
	}
	if out["opening_balance_kobo"] != float64(0) {
		t.Fatalf("expected opening_balance_kobo 0, got %v", out["opening_balance_kobo"])
	}

	entries := out["entries"].([]interface{})
	if len(entries) != 3 {
		t.Fatalf("expected 3 statement entries, got %d", len(entries))
	}

	// Entries are returned newest-first. Verify running balances:
	// entries[0] = 300 NGN credit → running_balance = 60000 (balance after this entry)
	// entries[1] = 200 NGN credit → running_balance = 30000
	// entries[2] = 100 NGN credit → running_balance = 10000
	expected := []struct {
		amount         float64
		runningBalance float64
	}{
		{30000, 60000},
		{20000, 30000},
		{10000, 10000},
	}

	for i, want := range expected {
		e := entries[i].(map[string]interface{})
		if e["direction"] != "credit" {
			t.Errorf("entry[%d]: expected direction credit, got %v", i, e["direction"])
		}
		if e["amount_kobo"] != want.amount {
			t.Errorf("entry[%d]: expected amount_kobo %v, got %v", i, want.amount, e["amount_kobo"])
		}
		if e["running_balance_kobo"] != want.runningBalance {
			t.Errorf("entry[%d]: expected running_balance_kobo %v, got %v", i, want.runningBalance, e["running_balance_kobo"])
		}
	}
}

// TestStatement_RunningBalanceAfterDebit verifies that a debit correctly
// reduces the running balance reported in the statement.
func TestStatement_RunningBalanceAfterDebit(t *testing.T) {
	env := newTestEnv(t)

	customer := createCustomer(t, env, 1)
	acct := createAccount(t, env, customer["id"].(string))
	accountID := acct["id"].(string)
	bankAccountNumber := acct["bank_account_number"].(string)

	// Fund 500 NGN
	body, sig, ts := webhookBody(t, "txn-stmt-debit-fund", bankAccountNumber, 500.00)
	postWebhook(t, env, body, sig, ts)
	time.Sleep(2 * time.Millisecond)

	// Transfer out 200 NGN = 20000 kobo
	env.do(t, http.MethodPost, "/accounts/"+accountID+"/transfer",
		transferPayload(20000, "ref-stmt-debit-001"), authed())

	resp := env.do(t, http.MethodGet, "/accounts/"+accountID+"/statement", nil, authed())
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)

	// 500 - 200 = 300 NGN = 30000 kobo
	if out["closing_balance_kobo"] != float64(30000) {
		t.Fatalf("expected closing_balance_kobo 30000, got %v", out["closing_balance_kobo"])
	}
	if out["opening_balance_kobo"] != float64(0) {
		t.Fatalf("expected opening_balance_kobo 0, got %v", out["opening_balance_kobo"])
	}

	entries := out["entries"].([]interface{})
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (1 credit + 1 debit), got %d", len(entries))
	}

	// Newest first: debit then credit
	debitEntry := entries[0].(map[string]interface{})
	creditEntry := entries[1].(map[string]interface{})

	if debitEntry["direction"] != "debit" {
		t.Fatalf("entries[0]: expected debit, got %v", debitEntry["direction"])
	}
	// Running balance after debit = 30000
	if debitEntry["running_balance_kobo"] != float64(30000) {
		t.Fatalf("entries[0] running_balance_kobo: expected 30000, got %v", debitEntry["running_balance_kobo"])
	}

	if creditEntry["direction"] != "credit" {
		t.Fatalf("entries[1]: expected credit, got %v", creditEntry["direction"])
	}
	// Running balance after credit = 50000
	if creditEntry["running_balance_kobo"] != float64(50000) {
		t.Fatalf("entries[1] running_balance_kobo: expected 50000, got %v", creditEntry["running_balance_kobo"])
	}
}

// TestStatement_Pagination verifies that page 2 entries have correct
// running balances relative to page 1, not starting from 0.
func TestStatement_Pagination(t *testing.T) {
	env := newTestEnv(t)

	customer := createCustomer(t, env, 1)
	acct := createAccount(t, env, customer["id"].(string))
	accountID := acct["id"].(string)
	bankAccountNumber := acct["bank_account_number"].(string)

	// Post 3 credits with distinct timestamps
	for i, txn := range []struct{ id string; naira float64 }{
		{"txn-page-001", 100},
		{"txn-page-002", 200},
		{"txn-page-003", 300},
	} {
		body, sig, ts := webhookBody(t, txn.id, bankAccountNumber, txn.naira)
		postWebhook(t, env, body, sig, ts)
		if i < 2 {
			time.Sleep(2 * time.Millisecond)
		}
	}

	// Page 1: limit=2, offset=0 — newest 2 entries (300 and 200 NGN)
	resp1 := env.do(t, http.MethodGet, "/accounts/"+accountID+"/statement?limit=2&offset=0", nil, authed())
	mustStatus(t, resp1, http.StatusOK)
	var p1 map[string]interface{}
	decodeJSON(t, resp1, &p1)

	p1entries := p1["entries"].([]interface{})
	if len(p1entries) != 2 {
		t.Fatalf("page 1: expected 2 entries, got %d", len(p1entries))
	}
	if p1["closing_balance_kobo"] != float64(60000) {
		t.Fatalf("page 1 closing_balance_kobo: expected 60000, got %v", p1["closing_balance_kobo"])
	}

	// Page 2: limit=2, offset=2 — oldest entry (100 NGN)
	resp2 := env.do(t, http.MethodGet, "/accounts/"+accountID+"/statement?limit=2&offset=2", nil, authed())
	mustStatus(t, resp2, http.StatusOK)
	var p2 map[string]interface{}
	decodeJSON(t, resp2, &p2)

	p2entries := p2["entries"].([]interface{})
	if len(p2entries) != 1 {
		t.Fatalf("page 2: expected 1 entry, got %d", len(p2entries))
	}

	// The page-2 entry is the 100 NGN credit. Its running_balance should be
	// 10000 (balance after this entry), not 60000.
	p2entry := p2entries[0].(map[string]interface{})
	if p2entry["running_balance_kobo"] != float64(10000) {
		t.Fatalf("page 2 entry running_balance_kobo: expected 10000, got %v", p2entry["running_balance_kobo"])
	}

	// Page-2 opening balance = 0 (nothing before the oldest entry)
	if p2["opening_balance_kobo"] != float64(0) {
		t.Fatalf("page 2 opening_balance_kobo: expected 0, got %v", p2["opening_balance_kobo"])
	}
}
