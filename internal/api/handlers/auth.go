package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"go.uber.org/zap"

	domainauth "github.com/abdulsalamcodes/ancra/internal/domain/auth"
	"github.com/abdulsalamcodes/ancra/internal/api/middleware"
)

// AuthHandler handles developer account registration and session management.
type AuthHandler struct {
	authSvc *domainauth.Service
	log     *zap.Logger
}

// NewAuthHandler constructs an AuthHandler.
func NewAuthHandler(authSvc *domainauth.Service, log *zap.Logger) *AuthHandler {
	return &AuthHandler{authSvc: authSvc, log: log}
}

// Signup handles POST /auth/signup — creates an org and its first owner user.
func (h *AuthHandler) Signup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OrgName  string `json:"org_name"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.OrgName == "" || body.Email == "" || body.Password == "" {
		writeError(w, http.StatusBadRequest, "org_name, email, and password are required")
		return
	}
	if len(body.Password) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	resp, err := h.authSvc.Signup(r.Context(), domainauth.SignupRequest{
		OrgName:  body.OrgName,
		Email:    body.Email,
		Password: body.Password,
	})
	if err != nil {
		if errors.Is(err, domainauth.ErrEmailTaken) {
			writeError(w, http.StatusConflict, "email already registered")
			return
		}
		h.log.Error("signup failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "signup failed")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"user":          resp.User,
		"org":           resp.Org,
		"access_token":  resp.AccessToken,
		"refresh_token": resp.RefreshToken,
	})
}

// Login handles POST /auth/login — validates credentials and issues tokens.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Email == "" || body.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	resp, err := h.authSvc.Login(r.Context(), domainauth.LoginRequest{
		Email:    body.Email,
		Password: body.Password,
	})
	if err != nil {
		if errors.Is(err, domainauth.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "invalid email or password")
			return
		}
		h.log.Error("login failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "login failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"user":          resp.User,
		"org":           resp.Org,
		"access_token":  resp.AccessToken,
		"refresh_token": resp.RefreshToken,
	})
}

// Refresh handles POST /auth/refresh — exchanges a refresh token for a new access token.
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, "refresh_token is required")
		return
	}

	accessToken, err := h.authSvc.RefreshAccess(r.Context(), body.RefreshToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "refresh token is invalid or expired")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"access_token": accessToken,
	})
}

// Logout handles POST /auth/logout — revokes the given refresh token.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.authSvc.Logout(r.Context(), body.RefreshToken); err != nil {
		h.log.Error("logout failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "logout failed")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Me handles GET /auth/me — returns the current user and their org.
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	rawUserID := middleware.UserIDFromContext(r.Context())
	userID, err := uuid.Parse(rawUserID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid session")
		return
	}

	user, org, err := h.authSvc.GetCurrentUser(r.Context(), userID)
	if err != nil {
		h.log.Error("me failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"user": user,
		"org":  org,
	})
}
