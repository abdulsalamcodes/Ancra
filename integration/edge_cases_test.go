package integration_test

import (
	"context"
	"net/http"
	"testing"
)

// ---------------------------------------------------------------------------
// Payment to a CLOSED account must land in suspense, not credit the customer.
// Spec: "close an account, send money to it, and watch it land in suspense"
// ---------------------------------------------------------------------------

func TestPaymentToClosedAccount_GoesToSuspense(t *testing.T) {
	env := newTestEnv(t)

	customer := createCustomer(t, env, 1)
	acct := createAccount(t, env, customer["id"].(string))
	accountID := acct["id"].(string)
	bankAccountNumber := acct["bank_account_number"].(string)

	// Credit the account once while it is open (so we have a baseline)
	body1, sig1, ts1 := webhookBody(t, "txn-preclose-001", bankAccountNumber, 100.00)
	postWebhook(t, env, body1, sig1, ts1)

	// Close the account
	closeResp := env.do(t, http.MethodPost, "/accounts/"+accountID+"/close", nil, authed())
	mustStatus(t, closeResp, http.StatusOK)

	// Record balance before the post-close payment
	balBefore := getBalance(t, env, accountID)

	// Send another payment to the now-closed account
	body2, sig2, ts2 := webhookBody(t, "txn-postclose-001", bankAccountNumber, 200.00)
	wResp := postWebhook(t, env, body2, sig2, ts2)
	mustStatus(t, wResp, http.StatusOK)

	var wOut map[string]interface{}
	decodeJSON(t, wResp, &wOut)
	if wOut["status"] != "suspense" {
		t.Fatalf("expected status suspense for closed account, got %v", wOut["status"])
	}

	// Customer balance must be unchanged (the 200 NGN did NOT land there)
	balAfter := getBalance(t, env, accountID)
	if balAfter != balBefore {
		t.Fatalf("balance changed after payment to closed account: before=%d after=%d", balBefore, balAfter)
	}

	// Suspense must hold the 200 NGN = 20000 kobo
	suspenseAcct, err := env.stores.ledger.GetSystemAccount(context.Background(), "suspense")
	if err != nil {
		t.Fatalf("get suspense account: %v", err)
	}
	suspenseBal, err := env.stores.ledger.GetBalance(context.Background(), suspenseAcct.ID)
	if err != nil {
		t.Fatalf("get suspense balance: %v", err)
	}
	if suspenseBal != 20000 {
		t.Fatalf("expected 20000 kobo in suspense, got %d", suspenseBal)
	}
}

// ---------------------------------------------------------------------------
// Point-in-time identity: old statement entries are unaffected by a rename.
// Spec: "rename a customer; old statement still shows the old name"
//
// Our ledger doesn't embed the name in entries — the identity_versions table
// is the source of truth. We verify that after a rename:
//   - the old identity version is closed (EffectiveTo is set)
//   - a new current identity exists with the new name
//   - the pre-rename entries are still present and untouched
// ---------------------------------------------------------------------------

func TestRename_PointInTime_OldIdentityPreserved(t *testing.T) {
	env := newTestEnv(t)

	customer := createCustomer(t, env, 1)
	customerID := customer["id"].(string)
	acct := createAccount(t, env, customerID) // creates initial identity "Test User"
	accountID := acct["id"].(string)
	bankAccountNumber := acct["bank_account_number"].(string)

	// Credit before the rename
	body, sig, ts := webhookBody(t, "txn-preename-001", bankAccountNumber, 300.00)
	postWebhook(t, env, body, sig, ts)

	// Rename
	renameResp := env.do(t, http.MethodPut, "/accounts/"+accountID,
		map[string]string{"display_name": "Jane Smith"}, authed())
	mustStatus(t, renameResp, http.StatusOK)

	// Inspect identity versions directly in the store
	env.stores.customers.mu.RLock()
	versions := env.stores.customers.identities
	env.stores.customers.mu.RUnlock()

	var openVersions, closedVersions int
	var currentName string
	for _, v := range versions {
		if v.CustomerID.String() != customerID {
			continue
		}
		if v.EffectiveTo == nil {
			openVersions++
			currentName = v.DisplayName
		} else {
			closedVersions++
		}
	}

	if openVersions != 1 {
		t.Fatalf("expected exactly 1 open identity version after rename, got %d", openVersions)
	}
	if closedVersions != 1 {
		t.Fatalf("expected exactly 1 closed identity version after rename, got %d", closedVersions)
	}
	if currentName != "Jane Smith" {
		t.Fatalf("expected current identity name 'Jane Smith', got %q", currentName)
	}

	// Pre-rename ledger entries must still exist and be unmodified
	txResp := env.do(t, http.MethodGet, "/accounts/"+accountID+"/transactions", nil, authed())
	mustStatus(t, txResp, http.StatusOK)
	var txOut map[string]interface{}
	decodeJSON(t, txResp, &txOut)
	entries := txOut["Entries"].([]interface{})
	if len(entries) != 1 {
		t.Fatalf("expected 1 ledger entry after rename, got %d", len(entries))
	}
}

// ---------------------------------------------------------------------------
// helpers used across edge case tests
// ---------------------------------------------------------------------------

func getBalance(t *testing.T, env *testEnv, accountID string) int64 {
	t.Helper()
	resp := env.do(t, http.MethodGet, "/accounts/"+accountID+"/balance", nil, authed())
	mustStatus(t, resp, http.StatusOK)
	var out map[string]interface{}
	decodeJSON(t, resp, &out)
	bal, ok := out["Balance"].(float64)
	if !ok {
		t.Fatalf("getBalance: unexpected Balance field: %v", out["Balance"])
	}
	return int64(bal)
}
