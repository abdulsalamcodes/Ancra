// Package worker contains background goroutines for Ancra.
package worker

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/abdulsalamcodes/ancra/internal/crypto"
	"github.com/abdulsalamcodes/ancra/internal/store"
)

const (
	maxAttempts    = 5
	initialBackoff = 30 * time.Second
	pollInterval   = 15 * time.Second
)

// OutboundWorker polls for pending webhook deliveries and dispatches them
// to each org's configured endpoint, signed with their unique secret.
type OutboundWorker struct {
	webhooks   store.WebhookStore
	configs    store.WebhookConfigStore
	encryptor  *crypto.Encryptor
	httpClient *http.Client
	log        *zap.Logger
}

// NewOutboundWorker constructs an OutboundWorker.
func NewOutboundWorker(
	webhooks store.WebhookStore,
	configs store.WebhookConfigStore,
	encryptor *crypto.Encryptor,
	log *zap.Logger,
) *OutboundWorker {
	return &OutboundWorker{
		webhooks:   webhooks,
		configs:    configs,
		encryptor:  encryptor,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		log:        log,
	}
}

// Run polls for pending webhook deliveries until ctx is cancelled.
func (w *OutboundWorker) Run(ctx context.Context) {
	w.log.Info("outbound webhook worker started", zap.Duration("poll_interval", pollInterval))

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.log.Info("outbound webhook worker stopping")
			return
		case <-ticker.C:
			w.dispatch(ctx)
		}
	}
}

func (w *OutboundWorker) dispatch(ctx context.Context) {
	deliveries, err := w.webhooks.ListPending(ctx, time.Now(), 50)
	if err != nil {
		w.log.Error("outbound: list pending deliveries failed", zap.Error(err))
		return
	}

	for _, d := range deliveries {
		if ctx.Err() != nil {
			return
		}
		w.deliver(ctx, d)
	}
}

func (w *OutboundWorker) deliver(ctx context.Context, d *store.WebhookDelivery) {
	d.Attempts++

	if err := w.post(ctx, d); err == nil {
		d.Status = store.WebhookStatusDelivered
		d.NextRetryAt = nil
		w.log.Info("outbound: delivery succeeded",
			zap.String("id", d.ID.String()),
			zap.String("event", d.EventType),
			zap.Int("attempts", d.Attempts),
		)
	} else {
		w.log.Warn("outbound: delivery failed",
			zap.String("id", d.ID.String()),
			zap.Int("attempts", d.Attempts),
			zap.Error(err),
		)
		w.scheduleRetry(d)
	}

	if updateErr := w.webhooks.UpdateDelivery(ctx, d); updateErr != nil {
		w.log.Error("outbound: update delivery record failed",
			zap.String("id", d.ID.String()), zap.Error(updateErr))
	}
}

func (w *OutboundWorker) scheduleRetry(d *store.WebhookDelivery) {
	if d.Attempts >= maxAttempts {
		d.Status = store.WebhookStatusFailed
		d.NextRetryAt = nil
		w.log.Error("outbound: delivery permanently failed — max attempts reached",
			zap.String("id", d.ID.String()))
		return
	}
	// Exponential back-off: 30s, 60s, 120s, 240s, 480s …
	backoff := initialBackoff * time.Duration(1<<uint(d.Attempts-1))
	next := time.Now().Add(backoff)
	d.NextRetryAt = &next
}

func (w *OutboundWorker) post(ctx context.Context, d *store.WebhookDelivery) error {
	cfg, err := w.configs.GetWebhookConfig(ctx, d.OrgID)
	if err != nil {
		// No webhook configured for this org — silently succeed so we don't
		// endlessly retry deliveries for orgs that haven't set an endpoint.
		w.log.Info("outbound: no webhook config for org — skipping",
			zap.String("org_id", d.OrgID.String()),
			zap.String("delivery_id", d.ID.String()),
		)
		return nil
	}

	signingSecret, err := w.encryptor.Decrypt(cfg.SigningSecretEncrypted)
	if err != nil {
		return fmt.Errorf("decrypt signing secret: %w", err)
	}

	signature := computeSignature(d.Payload, signingSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.EndpointURL, bytes.NewReader(d.Payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ancra-Event", d.EventType)
	req.Header.Set("X-Ancra-Delivery", d.ID.String())
	req.Header.Set("X-Ancra-Signature", "sha256="+signature)

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("endpoint returned non-2xx status: %d", resp.StatusCode)
	}
	return nil
}

// computeSignature returns the HMAC-SHA256 hex digest of payload keyed by secret.
// Format matches the GitHub webhook signature convention so developers recognise it.
func computeSignature(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}
