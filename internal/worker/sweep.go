// Package worker contains background goroutines for Ancra.
package worker

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/abdulsalamcodes/ancra/internal/domain/reconciliation"
	"github.com/abdulsalamcodes/ancra/internal/store"
)

// SweepWorker runs periodic reconciliation sweeps.
type SweepWorker struct {
	recon    *reconciliation.Service
	accounts store.AccountStore
	ledger   store.LedgerStore
	interval time.Duration
	log      *zap.Logger
}

// NewSweepWorker constructs a SweepWorker that fires every interval seconds.
func NewSweepWorker(
	recon *reconciliation.Service,
	accounts store.AccountStore,
	ledger store.LedgerStore,
	intervalSeconds int,
	log *zap.Logger,
) *SweepWorker {
	return &SweepWorker{
		recon:    recon,
		accounts: accounts,
		ledger:   ledger,
		interval: time.Duration(intervalSeconds) * time.Second,
		log:      log,
	}
}

// Run starts the sweep ticker. It blocks until ctx is cancelled.
func (w *SweepWorker) Run(ctx context.Context) {
	w.log.Info("sweep worker started", zap.Duration("interval", w.interval))
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	// Run once immediately so the first result is available straight away.
	w.runOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			w.log.Info("sweep worker stopping")
			return
		case <-ticker.C:
			w.runOnce(ctx)
		}
	}
}

func (w *SweepWorker) runOnce(ctx context.Context) {
	start := time.Now()
	w.log.Info("reconciliation sweep starting")

	// Back-fill any credits missed in the last 2× interval window.
	since := time.Now().UTC().Add(-2 * w.interval)
	if err := w.recon.BackfillMissedCredits(ctx, w.accounts, w.ledger, since); err != nil {
		w.log.Error("backfill failed", zap.Error(err))
		// Continue to the balance check even if backfill had errors.
	}

	run, err := w.recon.Run(ctx)
	if err != nil {
		w.log.Error("reconciliation run failed", zap.Error(err))
		return
	}

	w.log.Info("reconciliation sweep complete",
		zap.Duration("elapsed", time.Since(start)),
		zap.String("status", string(run.Status)),
		zap.Int64("nomba_balance_kobo", run.NombaWalletBalance),
		zap.Int64("pool_balance_kobo", run.ComputedPoolBalance),
		zap.Int64("delta_kobo", run.Delta),
	)
}
