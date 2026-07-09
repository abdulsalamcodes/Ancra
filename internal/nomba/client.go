package nomba

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Nomba API endpoint paths. All paths include their version prefix so the
// base URL remains version-agnostic (https://api.nomba.com).
// Nomba uses v1 for most resources and v2 for bank transfers.
const (
	endpointToken       = "/v1/auth/token/issue"
	endpointVirtualAcct = "/v1/accounts/virtual"
	endpointTransfer    = "/v2/transfers/bank"
	endpointBankLookup  = "/v1/transfers/bank/lookup"
	endpointBankList    = "/v1/transfers/bank"
)

// Client is an authenticated HTTP client for the Nomba API.
// It manages an OAuth2 client-credentials token with automatic refresh.
type Client struct {
	httpClient   *http.Client
	baseURL      string
	clientID     string
	clientSecret string
	accountID    string // parent account ID — used for token requests
	subAccountID string // sub-account ID — used for all resource operations
	log          *zap.Logger

	mu          sync.Mutex
	token       string
	tokenExpiry time.Time
}

// NewClient constructs a ready-to-use Nomba API client.
func NewClient(baseURL, clientID, clientSecret, accountID, subAccountID string, log *zap.Logger) *Client {
	return &Client{
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		baseURL:      baseURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		accountID:    accountID,
		subAccountID: subAccountID,
		log:          log,
	}
}

// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------

// GetToken returns a valid bearer token, refreshing it when it is within
// 60 seconds of expiry or has never been fetched.
func (c *Client) GetToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if time.Now().Add(60 * time.Second).Before(c.tokenExpiry) {
		return c.token, nil
	}

	c.log.Info("nomba: fetching new OAuth2 token")

	body := TokenRequest{
		GrantType:    "client_credentials",
		ClientID:     c.clientID,
		ClientSecret: c.clientSecret,
	}

	var resp TokenResponse
	// Token request uses the parent account ID in the header.
	if err := c.doJSON(ctx, http.MethodPost, endpointToken, "", c.accountID, body, &resp); err != nil {
		return "", fmt.Errorf("nomba: token refresh: %w", err)
	}

	if resp.Data.AccessToken == "" {
		return "", fmt.Errorf("nomba: token response missing access_token (code=%s, description=%s)",
			resp.Code, resp.Description)
	}
	c.token = resp.Data.AccessToken

	// Parse the ISO8601 expiry timestamp Nomba returns (e.g. "2026-07-07T05:42:45.029Z").
	expiry, parseErr := time.Parse(time.RFC3339, resp.Data.ExpiresAt)
	if parseErr != nil || expiry.IsZero() {
		expiry = time.Now().Add(3 * time.Hour) // safe fallback if timestamp is missing
	}
	c.tokenExpiry = expiry
	c.log.Info("nomba: token refreshed", zap.Time("expires_at", c.tokenExpiry))

	return c.token, nil
}

// ---------------------------------------------------------------------------
// Virtual accounts
// ---------------------------------------------------------------------------

// CreateVirtualAccount provisions a dedicated virtual account.
// Path: POST /accounts/virtual — accountId header uses the parent account ID.
func (c *Client) CreateVirtualAccount(ctx context.Context, req CreateVirtualAccountRequest) (*CreateVirtualAccountResponse, error) {
	token, err := c.GetToken(ctx)
	if err != nil {
		return nil, err
	}
	if req.Currency == "" {
		req.Currency = "NGN"
	}

	var resp CreateVirtualAccountResponse
	if err := c.doJSON(ctx, http.MethodPost, endpointVirtualAcct, token, c.accountID, req, &resp); err != nil {
		return nil, fmt.Errorf("nomba: create virtual account: %w", err)
	}
	if resp.Code != "00" {
		return nil, &APIError{Code: resp.Code, Description: resp.Description}
	}
	return &resp, nil
}

// ---------------------------------------------------------------------------
// Wallet balance
// ---------------------------------------------------------------------------

// GetWalletBalance returns the current balance of the sub-account wallet.
func (c *Client) GetWalletBalance(ctx context.Context) (*WalletBalanceResponse, error) {
	token, err := c.GetToken(ctx)
	if err != nil {
		return nil, err
	}

	var resp WalletBalanceResponse
	path := fmt.Sprintf("/v1/accounts/%s/balance", c.subAccountID)
	if err := c.doJSON(ctx, http.MethodGet, path, token, c.accountID, nil, &resp); err != nil {
		return nil, fmt.Errorf("nomba: get wallet balance: %w", err)
	}
	if resp.Code != "00" {
		return nil, &APIError{Code: resp.Code, Description: resp.Description}
	}
	return &resp, nil
}

// ---------------------------------------------------------------------------
// Transactions
// ---------------------------------------------------------------------------

// ListTransactions fetches a paginated list of transactions for the sub-account.
func (c *Client) ListTransactions(ctx context.Context, req ListTransactionsRequest) (*ListTransactionsResponse, error) {
	token, err := c.GetToken(ctx)
	if err != nil {
		return nil, err
	}

	q := url.Values{}
	if !req.StartDate.IsZero() {
		q.Set("startDate", req.StartDate.UTC().Format(time.RFC3339))
	}
	if !req.EndDate.IsZero() {
		q.Set("endDate", req.EndDate.UTC().Format(time.RFC3339))
	}
	if req.Page > 0 {
		q.Set("page", strconv.Itoa(req.Page))
	}
	if req.Limit > 0 {
		q.Set("limit", strconv.Itoa(req.Limit))
	}

	accountID := req.AccountID
	if accountID == "" {
		accountID = c.subAccountID
	}

	path := fmt.Sprintf("/v1/accounts/%s/transactions?%s", accountID, q.Encode())

	var resp ListTransactionsResponse
	if err := c.doJSON(ctx, http.MethodGet, path, token, c.subAccountID, nil, &resp); err != nil {
		return nil, fmt.Errorf("nomba: list transactions: %w", err)
	}
	if resp.Code != "00" {
		return nil, &APIError{Code: resp.Code, Description: resp.Description}
	}
	return &resp, nil
}

// ---------------------------------------------------------------------------
// Transfer
// ---------------------------------------------------------------------------

// Transfer initiates an outbound bank transfer from the sub-account wallet.
// Path: POST /v2/transfers/bank — amount must be in NGN (naira), not kobo.
func (c *Client) Transfer(ctx context.Context, req TransferRequest) (*TransferResponse, error) {
	token, err := c.GetToken(ctx)
	if err != nil {
		return nil, err
	}

	var resp TransferResponse
	if err := c.doJSON(ctx, http.MethodPost, endpointTransfer, token, c.subAccountID, req, &resp); err != nil {
		return nil, fmt.Errorf("nomba: transfer: %w", err)
	}
	if resp.Code != "00" {
		return nil, &APIError{Code: resp.Code, Description: resp.Description}
	}
	return &resp, nil
}

// LookupBankAccount resolves an account number + bank code to the account name.
// Path: POST /v1/transfers/bank/lookup
func (c *Client) LookupBankAccount(ctx context.Context, req BankLookupRequest) (*BankLookupResponse, error) {
	token, err := c.GetToken(ctx)
	if err != nil {
		return nil, err
	}

	var resp BankLookupResponse
	if err := c.doJSON(ctx, http.MethodPost, endpointBankLookup, token, c.subAccountID, req, &resp); err != nil {
		return nil, fmt.Errorf("nomba: bank lookup: %w", err)
	}
	if resp.Code != "00" {
		return nil, &APIError{Code: resp.Code, Description: resp.Description}
	}
	return &resp, nil
}

// ListBanks returns the current list of supported banks and their codes.
// Path: GET /v1/transfers/bank
func (c *Client) ListBanks(ctx context.Context) (*BankListResponse, error) {
	token, err := c.GetToken(ctx)
	if err != nil {
		return nil, err
	}

	var resp BankListResponse
	if err := c.doJSON(ctx, http.MethodGet, endpointBankList, token, c.subAccountID, nil, &resp); err != nil {
		return nil, fmt.Errorf("nomba: list banks: %w", err)
	}
	if resp.Code != "00" {
		return nil, &APIError{Code: resp.Code, Description: resp.Description}
	}
	return &resp, nil
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

// doJSON performs an HTTP request with the given accountId in the header.
// headerAccountID controls the Nomba-required accountId header independently
// from the path, allowing parent vs sub-account scoping per operation.
func (c *Client) doJSON(ctx context.Context, method, path, token, headerAccountID string, reqBody, respBody interface{}) error {
	var bodyReader io.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if headerAccountID != "" {
		req.Header.Set("accountId", headerAccountID)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	c.log.Info("nomba: http request",
		zap.String("method", method),
		zap.String("path", path),
		zap.String("accountId_header", headerAccountID),
	)

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http do: %w", err)
	}
	defer res.Body.Close()

	rawBody, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	c.log.Info("nomba: http response",
		zap.String("path", path),
		zap.Int("status", res.StatusCode),
		zap.String("body", string(rawBody)),
	)

	if res.StatusCode >= 400 {
		var apiErr APIError
		_ = json.Unmarshal(rawBody, &apiErr)
		if apiErr.Code == "" {
			apiErr.Code = strconv.Itoa(res.StatusCode)
			apiErr.Description = string(rawBody)
		}
		return &apiErr
	}

	if respBody != nil {
		if err := json.Unmarshal(rawBody, respBody); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}
