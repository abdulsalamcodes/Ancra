// Package auth implements developer account registration, login, and token management.
package auth

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"

	"github.com/abdulsalamcodes/ancra/internal/store"
)


// Sentinel errors returned by the service layer.
var (
	ErrEmailTaken         = errors.New("auth: email already registered")
	ErrInvalidCredentials = errors.New("auth: invalid email or password")
	ErrTokenInvalid       = errors.New("auth: refresh token is invalid or expired")
)

const bcryptCost = 12

// SignupRequest carries the fields required to create a new org and owner user.
type SignupRequest struct {
	OrgName  string
	Email    string
	Password string
}

// LoginRequest carries credentials for an existing user.
type LoginRequest struct {
	Email    string
	Password string
}

// AuthResponse is returned by Signup and Login — it includes the user, their
// org, and both tokens.
type AuthResponse struct {
	User         *store.User
	Org          *store.Organization
	AccessToken  string
	RefreshToken string // raw plaintext — shown to client once, never stored
}

// Service handles developer authentication and token lifecycle.
type Service struct {
	orgs          store.OrgStore
	users         store.UserStore
	refreshTokens store.RefreshTokenStore
	ledger        store.LedgerStore
	jwtSecret     []byte
	log           *zap.Logger
}

// NewService constructs an auth Service.
func NewService(
	orgs store.OrgStore,
	users store.UserStore,
	refreshTokens store.RefreshTokenStore,
	ledger store.LedgerStore,
	jwtSecret []byte,
	log *zap.Logger,
) *Service {
	return &Service{
		orgs:          orgs,
		users:         users,
		refreshTokens: refreshTokens,
		ledger:        ledger,
		jwtSecret:     jwtSecret,
		log:           log,
	}
}

// Signup creates a new organisation and its first owner user, then issues tokens.
func (s *Service) Signup(ctx context.Context, req SignupRequest) (*AuthResponse, error) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcryptCost)
	if err != nil {
		return nil, fmt.Errorf("auth.Signup: hash password: %w", err)
	}

	org := &store.Organization{
		ID:        uuid.New(),
		Name:      req.OrgName,
		Slug:      slugify(req.OrgName),
		CreatedAt: time.Now().UTC(),
	}
	if err := s.orgs.CreateOrg(ctx, org); err != nil {
		return nil, fmt.Errorf("auth.Signup: create org: %w", err)
	}

	if err := s.ledger.SeedSystemAccounts(ctx, org.ID); err != nil {
		return nil, fmt.Errorf("auth.Signup: seed system accounts: %w", err)
	}

	user := &store.User{
		ID:           uuid.New(),
		OrgID:        org.ID,
		Email:        strings.ToLower(strings.TrimSpace(req.Email)),
		PasswordHash: string(passwordHash),
		Role:         store.UserRoleOwner,
		CreatedAt:    time.Now().UTC(),
	}
	if err := s.users.CreateUser(ctx, user); err != nil {
		if strings.Contains(err.Error(), "unique") {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("auth.Signup: create user: %w", err)
	}

	return s.issueTokenPair(ctx, user, org)
}

// Login validates credentials and issues a new token pair.
func (s *Service) Login(ctx context.Context, req LoginRequest) (*AuthResponse, error) {
	email := strings.ToLower(strings.TrimSpace(req.Email))
	user, err := s.users.GetUserByEmail(ctx, email)
	if err != nil {
		// Constant-time: don't reveal whether email exists.
		_ = bcrypt.CompareHashAndPassword([]byte("$2a$12$placeholder"), []byte(req.Password))
		return nil, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	org, err := s.orgs.GetOrgByID(ctx, user.OrgID)
	if err != nil {
		return nil, fmt.Errorf("auth.Login: load org: %w", err)
	}

	return s.issueTokenPair(ctx, user, org)
}

// RefreshAccess exchanges a valid refresh token for a new access token.
func (s *Service) RefreshAccess(ctx context.Context, rawRefreshToken string) (string, error) {
	tokenHash := hashRefreshToken(rawRefreshToken)

	record, err := s.refreshTokens.GetRefreshTokenByHash(ctx, tokenHash)
	if err != nil {
		return "", ErrTokenInvalid
	}

	user, err := s.users.GetUserByID(ctx, record.UserID)
	if err != nil {
		return "", fmt.Errorf("auth.Refresh: load user: %w", err)
	}

	accessToken, err := issueAccessToken(user, s.jwtSecret)
	if err != nil {
		return "", err
	}
	return accessToken, nil
}

// Logout revokes the given refresh token, ending that session.
func (s *Service) Logout(ctx context.Context, rawRefreshToken string) error {
	tokenHash := hashRefreshToken(rawRefreshToken)

	record, err := s.refreshTokens.GetRefreshTokenByHash(ctx, tokenHash)
	if err != nil {
		// Token not found or already expired — treat as already logged out.
		return nil
	}
	return s.refreshTokens.RevokeRefreshToken(ctx, record.ID)
}

// GetCurrentUser returns the user and their org for an already-validated user ID.
func (s *Service) GetCurrentUser(ctx context.Context, userID uuid.UUID) (*store.User, *store.Organization, error) {
	user, err := s.users.GetUserByID(ctx, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("auth.Me: load user: %w", err)
	}
	org, err := s.orgs.GetOrgByID(ctx, user.OrgID)
	if err != nil {
		return nil, nil, fmt.Errorf("auth.Me: load org: %w", err)
	}
	return user, org, nil
}

// ParseToken validates a raw JWT string and returns the embedded claims.
func (s *Service) ParseToken(raw string) (*Claims, error) {
	return parseAccessToken(raw, s.jwtSecret)
}

// issueTokenPair creates a refresh token record and returns both tokens.
func (s *Service) issueTokenPair(ctx context.Context, user *store.User, org *store.Organization) (*AuthResponse, error) {
	accessToken, err := issueAccessToken(user, s.jwtSecret)
	if err != nil {
		return nil, err
	}

	rawRefresh, refreshHash, err := generateRefreshToken()
	if err != nil {
		return nil, err
	}

	record := newRefreshTokenRecord(user.ID, refreshHash)
	if err := s.refreshTokens.CreateRefreshToken(ctx, record); err != nil {
		return nil, fmt.Errorf("auth: persist refresh token: %w", err)
	}

	return &AuthResponse{
		User:         user,
		Org:          org,
		AccessToken:  accessToken,
		RefreshToken: rawRefresh,
	}, nil
}

// slugify converts an arbitrary org name into a URL-safe lowercase slug.
var nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(name string) string {
	slug := strings.ToLower(name)
	slug = nonSlugChars.ReplaceAllString(slug, "-")
	return strings.Trim(slug, "-")
}
