package nomba

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/abdulsalamcodes/ancra/internal/crypto"
	"github.com/abdulsalamcodes/ancra/internal/store"
)

const (
	cacheTTL           = 5 * time.Minute
	nombaSandboxBaseURL = "https://sandbox.nomba.com"
)

// cachedEntry holds a built client+verifier pair with an expiry timestamp.
type cachedEntry struct {
	client   *Client
	verifier *Verifier
	expiresAt time.Time
}

func (e *cachedEntry) isExpired() bool {
	return time.Now().After(e.expiresAt)
}

// ClientFactory builds and caches per-org Nomba clients from stored credentials.
// It decrypts secrets on demand using the provided Encryptor and caches the
// resulting clients for cacheTTL to avoid a DB round-trip on every request.
type ClientFactory struct {
	configs   store.NombaConfigStore
	encryptor *crypto.Encryptor
	baseURL   string
	log       *zap.Logger

	mu    sync.Mutex
	cache map[uuid.UUID]*cachedEntry
}

// NewClientFactory constructs a ClientFactory.
func NewClientFactory(
	configs store.NombaConfigStore,
	encryptor *crypto.Encryptor,
	baseURL string,
	log *zap.Logger,
) *ClientFactory {
	return &ClientFactory{
		configs:   configs,
		encryptor: encryptor,
		baseURL:   baseURL,
		log:       log,
		cache:     make(map[uuid.UUID]*cachedEntry),
	}
}

// ForOrg returns a Nomba Client configured with the given org's credentials.
// Results are cached for cacheTTL; call InvalidateOrg after a credential update.
func (f *ClientFactory) ForOrg(ctx context.Context, orgID uuid.UUID) (*Client, error) {
	if entry := f.fromCache(orgID); entry != nil {
		return entry.client, nil
	}

	entry, err := f.buildEntry(ctx, orgID)
	if err != nil {
		return nil, err
	}

	f.storeInCache(orgID, entry)
	return entry.client, nil
}

// VerifierForOrg returns a Nomba Verifier configured with the given org's
// webhook secret. Results are cached alongside the client.
func (f *ClientFactory) VerifierForOrg(ctx context.Context, orgID uuid.UUID) (*Verifier, error) {
	if entry := f.fromCache(orgID); entry != nil {
		return entry.verifier, nil
	}

	entry, err := f.buildEntry(ctx, orgID)
	if err != nil {
		return nil, err
	}

	f.storeInCache(orgID, entry)
	return entry.verifier, nil
}

// InvalidateOrg evicts an org's cached entry, forcing a fresh DB + decrypt on
// the next call. Call this after saving updated credentials.
func (f *ClientFactory) InvalidateOrg(orgID uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.cache, orgID)
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (f *ClientFactory) fromCache(orgID uuid.UUID) *cachedEntry {
	f.mu.Lock()
	defer f.mu.Unlock()

	entry, ok := f.cache[orgID]
	if !ok || entry.isExpired() {
		return nil
	}
	return entry
}

func (f *ClientFactory) storeInCache(orgID uuid.UUID, entry *cachedEntry) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cache[orgID] = entry
}

func (f *ClientFactory) buildEntry(ctx context.Context, orgID uuid.UUID) (*cachedEntry, error) {
	cfg, err := f.configs.GetNombaConfig(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("factory: load nomba config for org %s: %w", orgID, err)
	}

	clientSecret, err := f.encryptor.Decrypt(cfg.ClientSecretEncrypted)
	if err != nil {
		return nil, fmt.Errorf("factory: decrypt client_secret for org %s: %w", orgID, err)
	}

	webhookSecret, err := f.encryptor.Decrypt(cfg.WebhookSecretEncrypted)
	if err != nil {
		return nil, fmt.Errorf("factory: decrypt webhook_secret for org %s: %w", orgID, err)
	}

	baseURL := f.baseURL
	if cfg.Sandbox {
		baseURL = sandboxURL(baseURL)
	}

	client := NewClient(baseURL, cfg.ClientID, clientSecret, cfg.AccountID, cfg.SubAccountID, f.log)
	verifier := NewVerifier(webhookSecret)

	f.log.Info("nomba: built client for org",
		zap.String("org_id", orgID.String()),
		zap.Bool("sandbox", cfg.Sandbox),
	)

	return &cachedEntry{
		client:    client,
		verifier:  verifier,
		expiresAt: time.Now().Add(cacheTTL),
	}, nil
}

// sandboxURL returns the Nomba sandbox base URL.
// Sandbox is a separate host from production; credentials are not interchangeable.
func sandboxURL(_ string) string {
	return nombaSandboxBaseURL
}
