// Package identity manages customer identity versioning for Ancra.
// Display names are stored as append-only, time-bounded versions so that
// historical transaction statements can be rendered with the name that was
// active at the time of the transaction.
package identity

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/abdulsalamcodes/ancra/internal/store"
)

// Service manages customer identity versions.
type Service struct {
	customers store.CustomerStore
	log       *zap.Logger
}

// NewService constructs an identity Service.
func NewService(customers store.CustomerStore, log *zap.Logger) *Service {
	return &Service{customers: customers, log: log}
}

// RegisterCustomer creates a new customer record plus the initial identity version.
func (s *Service) RegisterCustomer(ctx context.Context, kycTier int, displayName string) (*store.Customer, *store.IdentityVersion, error) {
	now := time.Now().UTC()

	customer := &store.Customer{
		ID:        uuid.New(),
		KYCTier:   kycTier,
		CreatedAt: now,
	}
	if err := s.customers.CreateCustomer(ctx, customer); err != nil {
		return nil, nil, fmt.Errorf("identity.RegisterCustomer: %w", err)
	}

	iv := &store.IdentityVersion{
		ID:            uuid.New(),
		CustomerID:    customer.ID,
		DisplayName:   displayName,
		EffectiveFrom: now,
		EffectiveTo:   nil,
	}
	if err := s.customers.CreateIdentityVersion(ctx, iv); err != nil {
		return nil, nil, fmt.Errorf("identity.RegisterCustomer: identity version: %w", err)
	}

	s.log.Info("customer registered",
		zap.String("customer_id", customer.ID.String()),
		zap.String("display_name", displayName),
		zap.Int("kyc_tier", kycTier),
	)

	return customer, iv, nil
}

// UpdateDisplayName closes the current active identity version and opens a new
// one with the given display name.
func (s *Service) UpdateDisplayName(ctx context.Context, customerID uuid.UUID, newName string) (*store.IdentityVersion, error) {
	now := time.Now().UTC()

	current, err := s.customers.GetCurrentIdentity(ctx, customerID)
	if err != nil {
		return nil, fmt.Errorf("identity.UpdateDisplayName: current identity: %w", err)
	}

	// Close the current version.
	if err := s.customers.CloseIdentityVersion(ctx, current.ID, now); err != nil {
		return nil, fmt.Errorf("identity.UpdateDisplayName: close: %w", err)
	}

	// Open a new version.
	newVersion := &store.IdentityVersion{
		ID:            uuid.New(),
		CustomerID:    customerID,
		DisplayName:   newName,
		EffectiveFrom: now,
		EffectiveTo:   nil,
	}
	if err := s.customers.CreateIdentityVersion(ctx, newVersion); err != nil {
		return nil, fmt.Errorf("identity.UpdateDisplayName: new version: %w", err)
	}

	s.log.Info("identity updated",
		zap.String("customer_id", customerID.String()),
		zap.String("old_name", current.DisplayName),
		zap.String("new_name", newName),
	)

	return newVersion, nil
}

// GetCurrentIdentity returns the currently active identity for a customer.
func (s *Service) GetCurrentIdentity(ctx context.Context, customerID uuid.UUID) (*store.IdentityVersion, error) {
	iv, err := s.customers.GetCurrentIdentity(ctx, customerID)
	if err != nil {
		return nil, fmt.Errorf("identity.GetCurrentIdentity: %w", err)
	}
	return iv, nil
}
