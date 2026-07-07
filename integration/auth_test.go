package integration_test

import (
	"net/http"
	"testing"
)

func TestAuth_MissingHeader(t *testing.T) {
	env := newTestEnv(t)

	resp := env.do(t, http.MethodGet, "/customers", nil, nil)
	defer resp.Body.Close()
	mustStatus(t, resp, http.StatusUnauthorized)
}

func TestAuth_WrongKey(t *testing.T) {
	env := newTestEnv(t)

	resp := env.do(t, http.MethodGet, "/customers", nil,
		map[string]string{"Authorization": "Bearer wrong-key"})
	defer resp.Body.Close()
	mustStatus(t, resp, http.StatusForbidden)
}

func TestAuth_CorrectKey_Passes(t *testing.T) {
	env := newTestEnv(t)

	resp := env.do(t, http.MethodGet, "/customers", nil, authed())
	mustStatus(t, resp, http.StatusOK)
}

func TestAdminAuth_MissingSecret(t *testing.T) {
	env := newTestEnv(t)

	resp := env.do(t, http.MethodPost, "/admin/api-keys",
		map[string]string{"name": "test"}, nil)
	defer resp.Body.Close()
	mustStatus(t, resp, http.StatusForbidden)
}

func TestAdminAuth_WrongSecret(t *testing.T) {
	env := newTestEnv(t)

	resp := env.do(t, http.MethodPost, "/admin/api-keys",
		map[string]string{"name": "test"},
		map[string]string{"Admin-Secret": "wrong"})
	defer resp.Body.Close()
	mustStatus(t, resp, http.StatusForbidden)
}

func TestAdminAuth_CorrectSecret_Passes(t *testing.T) {
	env := newTestEnv(t)

	resp := env.do(t, http.MethodGet, "/admin/api-keys", nil, admin())
	mustStatus(t, resp, http.StatusOK)
}

func TestHealth_Unauthenticated(t *testing.T) {
	env := newTestEnv(t)

	resp := env.do(t, http.MethodGet, "/health", nil, nil)
	mustStatus(t, resp, http.StatusOK)

	var out map[string]interface{}
	decodeJSON(t, resp, &out)
	if out["status"] != "ok" {
		t.Fatalf("expected status ok, got %v", out["status"])
	}
}
