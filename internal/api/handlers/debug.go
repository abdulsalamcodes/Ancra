package handlers

import (
	"net/http"

	"go.uber.org/zap"

	"github.com/abdulsalamcodes/ancra/internal/nomba"
)

// DebugHandler exposes diagnostic endpoints that are only accessible to admins.
type DebugHandler struct {
	nomba *nomba.Client
	log   *zap.Logger
}

// NewDebugHandler constructs a DebugHandler.
func NewDebugHandler(n *nomba.Client, log *zap.Logger) *DebugHandler {
	return &DebugHandler{nomba: n, log: log}
}

// NombaDebug verifies Nomba connectivity and whether the configured account
// IDs actually exist and are accessible with the current credentials.
//
// GET /admin/debug/nomba
func (h *DebugHandler) NombaDebug(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	type accountResult struct {
		AccountID   string `json:"account_id"`
		Accessible  bool   `json:"accessible"`
		AccountName string `json:"account_name,omitempty"`
		AccountType string `json:"account_type,omitempty"`
		Status      string `json:"status,omitempty"`
		Error       string `json:"error,omitempty"`
	}

	parentID := h.nomba.ParentAccountID()
	subID := h.nomba.SubAccountID()

	// 1. Test token acquisition.
	token, tokenErr := h.nomba.GetToken(ctx)
	tokenOK := tokenErr == nil && token != ""
	tokenErrMsg := ""
	if tokenErr != nil {
		tokenErrMsg = tokenErr.Error()
	}

	// 2. Test parent account lookup using parent as header.
	parentResult := accountResult{AccountID: parentID}
	if tokenOK {
		info, err := h.nomba.GetAccount(ctx, parentID, parentID)
		if err != nil {
			parentResult.Error = err.Error()
		} else {
			parentResult.Accessible = true
			parentResult.AccountName = info.AccountName
			parentResult.AccountType = info.AccountType
			parentResult.Status = info.Status
		}
	}

	// 3. Test sub-account lookup using parent as header (most permissive).
	subWithParentHdr := accountResult{AccountID: subID}
	if tokenOK {
		info, err := h.nomba.GetAccount(ctx, subID, parentID)
		if err != nil {
			subWithParentHdr.Error = err.Error()
		} else {
			subWithParentHdr.Accessible = true
			subWithParentHdr.AccountName = info.AccountName
			subWithParentHdr.AccountType = info.AccountType
			subWithParentHdr.Status = info.Status
		}
	}

	// 4. Test sub-account lookup using sub as header.
	subWithSubHdr := accountResult{AccountID: subID}
	if tokenOK {
		info, err := h.nomba.GetAccount(ctx, subID, subID)
		if err != nil {
			subWithSubHdr.Error = err.Error()
		} else {
			subWithSubHdr.Accessible = true
			subWithSubHdr.AccountName = info.AccountName
			subWithSubHdr.AccountType = info.AccountType
			subWithSubHdr.Status = info.Status
		}
	}

	// Raw token probe — shows exact body Nomba returns so we can verify struct shape.
	rawStatus, rawBody, rawErr := h.nomba.FetchTokenRaw(r.Context())
	rawTokenErrMsg := ""
	if rawErr != nil {
		rawTokenErrMsg = rawErr.Error()
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"token_ok":                    tokenOK,
		"token_error":                 tokenErrMsg,
		"token_raw_status":            rawStatus,
		"token_raw_body":              rawBody,
		"token_raw_error":             rawTokenErrMsg,
		"parent_account":              parentResult,
		"sub_account_parent_header":   subWithParentHdr,
		"sub_account_sub_header":      subWithSubHdr,
	})
}
