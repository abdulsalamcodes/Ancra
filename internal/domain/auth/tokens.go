package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/abdulsalamcodes/ancra/internal/store"
)

const (
	accessTokenDuration  = 15 * time.Minute
	refreshTokenDuration = 30 * 24 * time.Hour
)

// Claims are the JWT payload fields embedded in every access token.
type Claims struct {
	UserID string `json:"user_id"`
	OrgID  string `json:"org_id"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// issueAccessToken signs a new short-lived JWT for the given user.
func issueAccessToken(u *store.User, secret []byte) (string, error) {
	now := time.Now().UTC()
	claims := Claims{
		UserID: u.ID.String(),
		OrgID:  u.OrgID.String(),
		Email:  u.Email,
		Role:   string(u.Role),
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(accessTokenDuration)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(secret)
	if err != nil {
		return "", fmt.Errorf("auth: sign access token: %w", err)
	}
	return signed, nil
}

// parseAccessToken validates a signed JWT and returns its claims.
func parseAccessToken(raw string, secret []byte) (*Claims, error) {
	token, err := jwt.ParseWithClaims(raw, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("auth: unexpected signing method: %v", t.Header["alg"])
		}
		return secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("auth: parse token: %w", err)
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("auth: invalid token claims")
	}
	return claims, nil
}

// generateRefreshToken returns a cryptographically random opaque token and
// its SHA-256 hash (the hash is what we persist; the raw value goes to the client).
func generateRefreshToken() (raw string, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", fmt.Errorf("auth: generate refresh token: %w", err)
	}
	raw = hex.EncodeToString(b)
	sum := sha256.Sum256([]byte(raw))
	hash = hex.EncodeToString(sum[:])
	return raw, hash, nil
}

// hashRefreshToken returns the SHA-256 hex digest of a raw refresh token.
// Used to look up a token the client sent without storing the plaintext.
func hashRefreshToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// newRefreshTokenRecord builds a store.RefreshToken ready for insertion.
func newRefreshTokenRecord(userID uuid.UUID, tokenHash string) *store.RefreshToken {
	now := time.Now().UTC()
	return &store.RefreshToken{
		ID:        uuid.New(),
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: now.Add(refreshTokenDuration),
		CreatedAt: now,
	}
}
