package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"go.uber.org/zap"

	"github.com/abdulsalamcodes/ancra/internal/crypto"
	"github.com/abdulsalamcodes/ancra/internal/store"
)

// WebhookConfigHandler manages per-org outbound webhook settings.
type WebhookConfigHandler struct {
	configs   store.WebhookConfigStore
	encryptor *crypto.Encryptor
	log       *zap.Logger
}

// NewWebhookConfigHandler constructs a WebhookConfigHandler.
func NewWebhookConfigHandler(
	configs store.WebhookConfigStore,
	encryptor *crypto.Encryptor,
	log *zap.Logger,
) *WebhookConfigHandler {
	return &WebhookConfigHandler{
		configs:   configs,
		encryptor: encryptor,
		log:       log,
	}
}

// Get returns the current outbound webhook config for the requesting org.
// The signing secret is never returned — it was shown once on creation.
//
// GET /settings/webhook
func (h *WebhookConfigHandler) Get(w http.ResponseWriter, r *http.Request) {
	orgID, ok := requireOrgID(w, r)
	if !ok {
		return
	}

	cfg, err := h.configs.GetWebhookConfig(r.Context(), orgID)
	if err != nil {
		h.log.Error("settings: get webhook config", zap.Error(err))
		writeError(w, http.StatusNotFound, "webhook config not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"org_id":       cfg.OrgID,
		"endpoint_url": cfg.EndpointURL,
		"updated_at":   cfg.UpdatedAt,
	})
}

// Upsert sets or replaces the outbound webhook endpoint for the requesting org.
// A fresh signing secret is generated and returned in the response exactly once.
// If an endpoint already exists, the signing secret is rotated on every PUT.
//
// PUT /settings/webhook
func (h *WebhookConfigHandler) Upsert(w http.ResponseWriter, r *http.Request) {
	orgID, ok := requireOrgID(w, r)
	if !ok {
		return
	}

	var body struct {
		EndpointURL string `json:"endpoint_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.EndpointURL == "" {
		writeError(w, http.StatusBadRequest, "endpoint_url is required")
		return
	}

	rawSecret, encryptedSecret, err := generateAndEncryptSecret(h.encryptor)
	if err != nil {
		h.log.Error("settings: generate webhook signing secret", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to generate signing secret")
		return
	}

	cfg := &store.OrgWebhookConfig{
		OrgID:                 orgID,
		EndpointURL:           body.EndpointURL,
		SigningSecretEncrypted: encryptedSecret,
	}
	if err := h.configs.UpsertWebhookConfig(r.Context(), cfg); err != nil {
		h.log.Error("settings: upsert webhook config", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to save webhook config")
		return
	}

	h.log.Info("settings: webhook config saved",
		zap.String("org_id", orgID.String()),
		zap.String("endpoint_url", body.EndpointURL),
	)

	// Return the raw secret once — it will never be shown again.
	writeJSON(w, http.StatusOK, map[string]string{
		"endpoint_url":   body.EndpointURL,
		"signing_secret": rawSecret,
		"note":           "Save this signing secret — it will not be shown again.",
	})
}

// generateAndEncryptSecret creates a 32-byte random hex secret and returns both
// the raw plaintext (for the response) and the AES-256-GCM encrypted form (for storage).
func generateAndEncryptSecret(encryptor *crypto.Encryptor) (rawSecret, encryptedSecret string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	rawSecret = hex.EncodeToString(buf)

	encryptedSecret, err = encryptor.Encrypt(rawSecret)
	if err != nil {
		return "", "", err
	}
	return rawSecret, encryptedSecret, nil
}
