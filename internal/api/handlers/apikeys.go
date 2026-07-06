package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/abdulsalamcodes/ancra/internal/api/middleware"
	"github.com/abdulsalamcodes/ancra/internal/store"
)

// APIKeyHandler manages developer API keys.
type APIKeyHandler struct {
	keys store.APIKeyStore
	log  *zap.Logger
}

// NewAPIKeyHandler constructs an APIKeyHandler.
func NewAPIKeyHandler(keys store.APIKeyStore, log *zap.Logger) *APIKeyHandler {
	return &APIKeyHandler{keys: keys, log: log}
}

// Create generates a new API key.
//
// POST /admin/api-keys
// Body: {"name": "my integration"}
func (h *APIKeyHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	// Generate 32 random bytes → hex string → prefix with "ancra_"
	raw, err := generateRawKey()
	if err != nil {
		h.log.Error("failed to generate api key", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to generate key")
		return
	}

	sum := sha256.Sum256([]byte(raw))
	hash := hex.EncodeToString(sum[:])

	k := &store.APIKey{
		ID:        uuid.New(),
		Name:      body.Name,
		KeyHash:   hash,
		CreatedAt: time.Now().UTC(),
	}
	if err := h.keys.CreateKey(r.Context(), k); err != nil {
		h.log.Error("failed to store api key", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to create key")
		return
	}

	h.log.Info("api key created", zap.String("id", k.ID.String()), zap.String("name", k.Name))

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":         k.ID,
		"name":       k.Name,
		"key":        raw, // shown once — never stored in plain text
		"created_at": k.CreatedAt,
	})
}

// List returns all API keys (hashes are never returned).
//
// GET /admin/api-keys
func (h *APIKeyHandler) List(w http.ResponseWriter, r *http.Request) {
	keys, err := h.keys.ListKeys(r.Context())
	if err != nil {
		h.log.Error("failed to list api keys", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to list keys")
		return
	}

	type keyView struct {
		ID         uuid.UUID  `json:"id"`
		Name       string     `json:"name"`
		CreatedAt  time.Time  `json:"created_at"`
		LastUsedAt *time.Time `json:"last_used_at"`
		RevokedAt  *time.Time `json:"revoked_at"`
	}
	views := make([]keyView, len(keys))
	for i, k := range keys {
		views[i] = keyView{
			ID:         k.ID,
			Name:       k.Name,
			CreatedAt:  k.CreatedAt,
			LastUsedAt: k.LastUsedAt,
			RevokedAt:  k.RevokedAt,
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"keys": views})
}

// Revoke marks an API key as revoked.
//
// DELETE /admin/api-keys/{id}
func (h *APIKeyHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid key id")
		return
	}

	// Fetch hash before revoking so we can invalidate the cache.
	k, err := h.keys.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "key not found")
		return
	}

	if err := h.keys.RevokeKey(r.Context(), id); err != nil {
		h.log.Error("failed to revoke api key", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to revoke key")
		return
	}

	// Immediately evict from cache so the key stops working right away.
	middleware.InvalidateKey(k.KeyHash)

	h.log.Info("api key revoked", zap.String("id", id.String()))
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func generateRawKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "ancra_" + hex.EncodeToString(b), nil
}
