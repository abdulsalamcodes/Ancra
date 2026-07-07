package integration_test

import (
	"net/http"
	"testing"
)

// TestAPIKey_FullLifecycle covers the complete flow:
// create key → authenticate with it → revoke it → key no longer works.
//
// Spec: "Developer API quality — idempotent create, paginated transactions,
// signed webhooks, sandbox mode"
func TestAPIKey_FullLifecycle(t *testing.T) {
	env := newTestEnv(t)

	// 1. Create a new API key via the admin endpoint.
	createResp := env.do(t, http.MethodPost, "/admin/api-keys",
		map[string]string{"name": "integration-test-key", "org_id": testOrgID}, admin())
	mustStatus(t, createResp, http.StatusCreated)

	var created map[string]interface{}
	decodeJSON(t, createResp, &created)

	rawKey, ok := created["key"].(string)
	if !ok || rawKey == "" {
		t.Fatalf("expected a non-empty key string in response, got: %v", created)
	}
	keyID, ok := created["id"].(string)
	if !ok || keyID == "" {
		t.Fatalf("expected a non-empty id in response, got: %v", created)
	}
	if created["name"] != "integration-test-key" {
		t.Fatalf("expected name integration-test-key, got %v", created["name"])
	}

	// 2. Authenticate using the new key — should succeed.
	resp := env.do(t, http.MethodGet, "/customers", nil,
		map[string]string{"Authorization": "Bearer " + rawKey})
	mustStatus(t, resp, http.StatusOK)

	// 3. Revoke the key.
	revokeResp := env.do(t, http.MethodDelete, "/admin/api-keys/"+keyID, nil, admin())
	mustStatus(t, revokeResp, http.StatusOK)

	var revokeOut map[string]interface{}
	decodeJSON(t, revokeResp, &revokeOut)
	if revokeOut["status"] != "revoked" {
		t.Fatalf("expected status revoked, got %v", revokeOut["status"])
	}

	// 4. The same key must now be rejected.
	rejResp := env.do(t, http.MethodGet, "/customers", nil,
		map[string]string{"Authorization": "Bearer " + rawKey})
	defer rejResp.Body.Close()
	if rejResp.StatusCode != http.StatusForbidden && rejResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401/403 after revocation, got %d", rejResp.StatusCode)
	}
}

// TestAPIKey_List returns all keys via GET /admin/api-keys.
func TestAPIKey_List(t *testing.T) {
	env := newTestEnv(t)

	// Create two keys
	env.do(t, http.MethodPost, "/admin/api-keys",
		map[string]string{"name": "key-one", "org_id": testOrgID}, admin())
	env.do(t, http.MethodPost, "/admin/api-keys",
		map[string]string{"name": "key-two", "org_id": testOrgID}, admin())

	resp := env.do(t, http.MethodGet, "/admin/api-keys", nil, admin())
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)
	keys := out["keys"].([]interface{})
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}

	// Key hashes must never appear in the list response
	for _, k := range keys {
		km := k.(map[string]interface{})
		if _, hasHash := km["key_hash"]; hasHash {
			t.Fatal("key_hash must not be returned in list response")
		}
		if _, hasKey := km["key"]; hasKey {
			t.Fatal("raw key must not be returned in list response")
		}
	}
}

// TestAPIKey_MissingName returns 400.
func TestAPIKey_MissingName(t *testing.T) {
	env := newTestEnv(t)

	resp := env.do(t, http.MethodPost, "/admin/api-keys",
		map[string]string{}, admin())
	defer resp.Body.Close()
	mustStatus(t, resp, http.StatusBadRequest)
}

// TestAPIKey_RevokeNonexistent returns 404.
func TestAPIKey_RevokeNonexistent(t *testing.T) {
	env := newTestEnv(t)

	resp := env.do(t, http.MethodDelete, "/admin/api-keys/00000000-0000-0000-0000-000000000099",
		nil, admin())
	defer resp.Body.Close()
	mustStatus(t, resp, http.StatusNotFound)
}
