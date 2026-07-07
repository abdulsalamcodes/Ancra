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
	if err := c.doJSON(ctx, http.MethodPost, "/auth/token/issue", "", c.accountID, body, &resp); err != nil {
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
	if err := c.doJSON(ctx, http.MethodPost, "/accounts/virtual", token, c.accountID, req, &resp); err != nil {
		return nil, fmt.Errorf("nomba: create virtual account: %w", err)
	}
	if resp.Code != "00" {
		return nil, &APIError{Code: resp.Code, Description: resp.Description}
	}
	return &resp, nil
}

// ---------------------------------------------------------------------------
// Account lookup (diagnostic)
// ---------------------------------------------------------------------------

// AccountInfo is a minimal Nomba account response used for diagnostics.
type AccountInfo struct {
	AccountID   string `json:"accountId"`
	AccountName string `json:"accountName"`
	AccountType string `json:"accountType"`
	Status      string `json:"status"`
}

type accountInfoResponse struct {
	Code        string      `json:"code"`
	Description string      `json:"description"`
	Data        AccountInfo `json:"data"`
}

// GetAccount fetches basic account details from Nomba. headerAccountID is the
// accountId header to send (use parent or sub depending on what you are testing).
func (c *Client) GetAccount(ctx context.Context, accountID, headerAccountID string) (*AccountInfo, error) {
	token, err := c.GetToken(ctx)
	if err != nil {
		return nil, err
	}
	var resp accountInfoResponse
	path := "/accounts/" + accountID
	if err := c.doJSON(ctx, http.MethodGet, path, token, headerAccountID, nil, &resp); err != nil {
		return nil, fmt.Errorf("nomba: get account: %w", err)
	}
	if resp.Code != "00" {
		return nil, &APIError{Code: resp.Code, Description: resp.Description}
	}
	return &resp.Data, nil
}

// ParentAccountID returns the parent account ID configured in the client.
func (c *Client) ParentAccountID() string { return c.accountID }

// SubAccountID returns the sub-account ID configured in the client.
func (c *Client) SubAccountID() string { return c.subAccountID }

// RawRequest performs an arbitrary authenticated request and returns the raw
// HTTP status and response body. Used only for diagnostic endpoints.
func (c *Client) RawRequest(ctx context.Context, method, path, headerAccountID string, reqBody interface{}) (int, string, error) {
	token, err := c.GetToken(ctx)
	if err != nil {
		return 0, "", fmt.Errorf("get token: %w", err)
	}

	var bodyReader io.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return 0, "", err
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	if headerAccountID != "" {
		req.Header.Set("accountId", headerAccountID)
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	return res.StatusCode, string(raw), err
}

// FetchTokenRaw performs the token request and returns the raw HTTP status
// and response body without any parsing — useful for diagnosing struct mismatches.
func (c *Client) FetchTokenRaw(ctx context.Context) (int, string, error) {
	body := TokenRequest{
		GrantType:    "client_credentials",
		ClientID:     c.clientID,
		ClientSecret: c.clientSecret,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return 0, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/auth/token/issue", bytes.NewReader(b))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.accountID != "" {
		req.Header.Set("accountId", c.accountID)
	}
	res, err := c.httpClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	return res.StatusCode, string(raw), err
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
	path := fmt.Sprintf("/accounts/%s/balance", c.subAccountID)
	if err := c.doJSON(ctx, http.MethodGet, path, token, c.subAccountID, nil, &resp); err != nil {
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

	path := fmt.Sprintf("/accounts/%s/transactions?%s", accountID, q.Encode())

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
func (c *Client) Transfer(ctx context.Context, req TransferRequest) (*TransferResponse, error) {
	token, err := c.GetToken(ctx)
	if err != nil {
		return nil, err
	}

	var resp TransferResponse
	path := fmt.Sprintf("/accounts/%s/transfers", c.subAccountID)
	if err := c.doJSON(ctx, http.MethodPost, path, token, c.subAccountID, req, &resp); err != nil {
		return nil, fmt.Errorf("nomba: transfer: %w", err)
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
