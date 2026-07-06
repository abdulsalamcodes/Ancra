package worker

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/abdulsalamcodes/ancra/internal/store"
)

const (
	maxAttempts    = 5
	initialBackoff = 30 * time.Second
	pollInterval   = 15 * time.Second
)

// OutboundWorker polls for pending webhook deliveries and dispatches them
// to the developer's registered endpoint with exponential back-off.
type OutboundWorker struct {
	webhooks    store.WebhookStore
	endpointURL string
	apiKey      string
	httpClient  *http.Client
	log         *zap.Logger
}

// NewOutboundWorker constructs an OutboundWorker.
func NewOutboundWorker(
	webhooks store.WebhookStore,
	endpointURL string,
	apiKey string,
	log *zap.Logger,
) *OutboundWorker {
	return &OutboundWorker{
		webhooks:    webhooks,
		endpointURL: endpointURL,
		apiKey:      apiKey,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		log:         log,
	}
}

// Run polls for pending webhook deliveries until ctx is cancelled.
func (w *OutboundWorker) Run(ctx context.Context) {
	w.log.Info("outbound webhook worker started",
		zap.String("endpoint", w.endpointURL),
		zap.Duration("poll_interval", pollInterval),
	)

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

	err := w.post(ctx, d)
	if err == nil {
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

		if d.Attempts >= maxAttempts {
			d.Status = store.WebhookStatusFailed
			d.NextRetryAt = nil
			w.log.Error("outbound: delivery permanently failed — max attempts reached",
				zap.String("id", d.ID.String()))
		} else {
			// Exponential back-off: 30s, 60s, 120s, 240s, 480s …
			backoff := initialBackoff * time.Duration(1<<uint(d.Attempts-1))
			next := time.Now().Add(backoff)
			d.NextRetryAt = &next
		}
	}

	if updateErr := w.webhooks.UpdateDelivery(ctx, d); updateErr != nil {
		w.log.Error("outbound: update delivery record failed",
			zap.String("id", d.ID.String()), zap.Error(updateErr))
	}
}

func (w *OutboundWorker) post(ctx context.Context, d *store.WebhookDelivery) error {
	if w.endpointURL == "" {
		return nil // no endpoint configured; silently succeed
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.endpointURL, bytes.NewReader(d.Payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ancra-Event", d.EventType)
	req.Header.Set("X-Ancra-Delivery", d.ID.String())
	if w.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+w.apiKey)
	}

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

