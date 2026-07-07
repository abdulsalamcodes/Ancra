package handlers

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"

	"github.com/abdulsalamcodes/ancra/internal/crypto"
	"github.com/abdulsalamcodes/ancra/internal/nomba"
	"github.com/abdulsalamcodes/ancra/internal/store"
)

// NombaConfigHandler manages per-org Nomba BYOK credential settings.
type NombaConfigHandler struct {
	configs   store.NombaConfigStore
	encryptor *crypto.Encryptor
	factory   *nomba.ClientFactory
	log       *zap.Logger
}

// NewNombaConfigHandler constructs a NombaConfigHandler.
func NewNombaConfigHandler(
	configs store.NombaConfigStore,
	encryptor *crypto.Encryptor,
	factory *nomba.ClientFactory,
	log *zap.Logger,
) *NombaConfigHandler {
	return &NombaConfigHandler{
		configs:   configs,
		encryptor: encryptor,
		factory:   factory,
		log:       log,
	}
}

// upsertRequest is the request body for PUT /settings/nomba.
type upsertRequest struct {
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"`
	AccountID     string `json:"account_id"`
	SubAccountID  string `json:"sub_account_id"`
	WebhookSecret string `json:"webhook_secret"`
	Sandbox       bool   `json:"sandbox"`
}

func (req *upsertRequest) validate() string {
	switch {
	case req.ClientID == "":
		return "client_id is required"
	case req.ClientSecret == "":
		return "client_secret is required"
	case req.AccountID == "":
		return "account_id is required"
	case req.SubAccountID == "":
		return "sub_account_id is required"
	case req.WebhookSecret == "":
		return "webhook_secret is required"
	default:
		return ""
	}
}

// Get returns the current Nomba config for the requesting org (secrets redacted).
//
// GET /settings/nomba
func (h *NombaConfigHandler) Get(w http.ResponseWriter, r *http.Request) {
	orgID, ok := requireOrgID(w, r)
	if !ok {
		return
	}

	cfg, err := h.configs.GetNombaConfig(r.Context(), orgID)
	if err != nil {
		h.log.Error("settings: get nomba config", zap.Error(err))
		writeError(w, http.StatusNotFound, "nomba config not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"org_id":       cfg.OrgID,
		"client_id":    cfg.ClientID,
		"account_id":   cfg.AccountID,
		"sub_account_id": cfg.SubAccountID,
		"sandbox":      cfg.Sandbox,
		"updated_at":   cfg.UpdatedAt,
	})
}

// Upsert creates or replaces the Nomba config for the requesting org.
//
// PUT /settings/nomba
func (h *NombaConfigHandler) Upsert(w http.ResponseWriter, r *http.Request) {
	orgID, ok := requireOrgID(w, r)
	if !ok {
		return
	}

	var req upsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if msg := req.validate(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	encryptedSecret, err := h.encryptor.Encrypt(req.ClientSecret)
	if err != nil {
		h.log.Error("settings: encrypt client_secret", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to encrypt credentials")
		return
	}

	encryptedWebhook, err := h.encryptor.Encrypt(req.WebhookSecret)
	if err != nil {
		h.log.Error("settings: encrypt webhook_secret", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to encrypt credentials")
		return
	}

	cfg := &store.OrgNombaConfig{
		OrgID:                  orgID,
		ClientID:               req.ClientID,
		ClientSecretEncrypted:  encryptedSecret,
		AccountID:              req.AccountID,
		SubAccountID:           req.SubAccountID,
		WebhookSecretEncrypted: encryptedWebhook,
		Sandbox:                req.Sandbox,
	}

	if err := h.configs.UpsertNombaConfig(r.Context(), cfg); err != nil {
		h.log.Error("settings: upsert nomba config", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to save config")
		return
	}

	// Evict the cache so the next request uses the updated credentials.
	h.factory.InvalidateOrg(orgID)

	h.log.Info("settings: nomba config saved", zap.String("org_id", orgID.String()))
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// TestConnection verifies that the stored credentials can authenticate with Nomba.
//
// POST /settings/nomba/test
func (h *NombaConfigHandler) TestConnection(w http.ResponseWriter, r *http.Request) {
	orgID, ok := requireOrgID(w, r)
	if !ok {
		return
	}

	client, err := h.factory.ForOrg(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "nomba config not found or invalid")
		return
	}

	if _, err := client.GetToken(r.Context()); err != nil {
		h.log.Warn("settings: nomba test connection failed",
			zap.String("org_id", orgID.String()), zap.Error(err))
		writeError(w, http.StatusBadGateway, "nomba authentication failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
