// Package worker contains background goroutines for Ancra.
package worker

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/abdulsalamcodes/ancra/internal/domain/reconciliation"
	"github.com/abdulsalamcodes/ancra/internal/store"
)

// SweepWorker runs periodic reconciliation sweeps across all registered orgs.
type SweepWorker struct {
	recon    *reconciliation.Service
	orgs     store.OrgStore
	accounts store.AccountStore
	ledger   store.LedgerStore
	interval time.Duration
	log      *zap.Logger
}

// NewSweepWorker constructs a SweepWorker that fires every intervalSeconds.
func NewSweepWorker(
	recon *reconciliation.Service,
	orgs store.OrgStore,
	accounts store.AccountStore,
	ledger store.LedgerStore,
	intervalSeconds int,
	log *zap.Logger,
) *SweepWorker {
	return &SweepWorker{
		recon:    recon,
		orgs:     orgs,
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
	w.sweepAllOrgs(ctx)

	for {
		select {
		case <-ctx.Done():
			w.log.Info("sweep worker stopping")
			return
		case <-ticker.C:
			w.sweepAllOrgs(ctx)
		}
	}
}

func (w *SweepWorker) sweepAllOrgs(ctx context.Context) {
	allOrgs, err := w.orgs.ListAllOrgs(ctx)
	if err != nil {
		w.log.Error("sweep: failed to list orgs", zap.Error(err))
		return
	}
	for _, org := range allOrgs {
		w.sweepOrg(ctx, org.ID)
	}
}

func (w *SweepWorker) sweepOrg(ctx context.Context, orgID uuid.UUID) {
	start := time.Now()
	w.log.Info("reconciliation sweep starting", zap.String("org_id", orgID.String()))

	since := time.Now().UTC().Add(-2 * w.interval)
	if err := w.recon.BackfillMissedCredits(ctx, orgID, w.accounts, w.ledger, since); err != nil {
		w.log.Error("backfill failed",
			zap.String("org_id", orgID.String()), zap.Error(err))
		// Continue to the balance check even if backfill had errors.
	}

	run, err := w.recon.Run(ctx, orgID)
	if err != nil {
		w.log.Error("reconciliation run failed",
			zap.String("org_id", orgID.String()), zap.Error(err))
		return
	}

	w.log.Info("reconciliation sweep complete",
		zap.String("org_id", orgID.String()),
		zap.Duration("elapsed", time.Since(start)),
		zap.String("status", string(run.Status)),
		zap.Int64("nomba_balance_kobo", run.NombaWalletBalance),
		zap.Int64("pool_balance_kobo", run.ComputedPoolBalance),
		zap.Int64("delta_kobo", run.Delta),
	)
}
