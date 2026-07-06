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
	httpClient  *http.Client
	baseURL     string
	clientID    string
	clientSecret string
	accountID   string
	log         *zap.Logger

	mu          sync.Mutex
	token       string
	tokenExpiry time.Time
}

// NewClient constructs a ready-to-use Nomba API client.
func NewClient(baseURL, clientID, clientSecret, accountID string, log *zap.Logger) *Client {
	return &Client{
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		baseURL:      baseURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		accountID:    accountID,
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
	if err := c.doJSON(ctx, http.MethodPost, "/auth/token/issue", "", body, &resp); err != nil {
		return "", fmt.Errorf("nomba: token refresh: %w", err)
	}

	c.token = resp.AccessToken
	// Nomba returns ExpiresIn in seconds.
	c.tokenExpiry = time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
	c.log.Info("nomba: token refreshed", zap.Time("expires_at", c.tokenExpiry))

	return c.token, nil
}

// ---------------------------------------------------------------------------
// Virtual accounts
// ---------------------------------------------------------------------------

// CreateVirtualAccount provisions a dedicated virtual account under your Nomba
// merchant account.
func (c *Client) CreateVirtualAccount(ctx context.Context, req CreateVirtualAccountRequest) (*CreateVirtualAccountResponse, error) {
	token, err := c.GetToken(ctx)
	if err != nil {
		return nil, err
	}

	var resp CreateVirtualAccountResponse
	path := fmt.Sprintf("/accounts/%s/virtual-accounts", c.accountID)
	if err := c.doJSON(ctx, http.MethodPost, path, token, req, &resp); err != nil {
		return nil, fmt.Errorf("nomba: create virtual account: %w", err)
	}
	if !resp.RequestSuccessful {
		return nil, &APIError{
			ResponseCode:    resp.ResponseCode,
			ResponseMessage: resp.ResponseMessage,
		}
	}
	return &resp, nil
}

// ---------------------------------------------------------------------------
// Wallet balance
// ---------------------------------------------------------------------------

// GetWalletBalance returns the current balance of the master Nomba wallet.
func (c *Client) GetWalletBalance(ctx context.Context) (*WalletBalanceResponse, error) {
	token, err := c.GetToken(ctx)
	if err != nil {
		return nil, err
	}

	var resp WalletBalanceResponse
	path := fmt.Sprintf("/accounts/%s/balance", c.accountID)
	if err := c.doJSON(ctx, http.MethodGet, path, token, nil, &resp); err != nil {
		return nil, fmt.Errorf("nomba: get wallet balance: %w", err)
	}
	if !resp.RequestSuccessful {
		return nil, &APIError{
			ResponseCode:    resp.ResponseCode,
			ResponseMessage: resp.ResponseMessage,
		}
	}
	return &resp, nil
}

// ---------------------------------------------------------------------------
// Transactions
// ---------------------------------------------------------------------------

// ListTransactions fetches a paginated list of transactions for the master
// Nomba account within the requested time window.
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
		accountID = c.accountID
	}

	path := fmt.Sprintf("/accounts/%s/transactions?%s", accountID, q.Encode())

	var resp ListTransactionsResponse
	if err := c.doJSON(ctx, http.MethodGet, path, token, nil, &resp); err != nil {
		return nil, fmt.Errorf("nomba: list transactions: %w", err)
	}
	if !resp.RequestSuccessful {
		return nil, &APIError{
			ResponseCode:    resp.ResponseCode,
			ResponseMessage: resp.ResponseMessage,
		}
	}
	return &resp, nil
}

// ---------------------------------------------------------------------------
// Transfer
// ---------------------------------------------------------------------------

// Transfer initiates an outbound bank transfer from the Nomba wallet.
func (c *Client) Transfer(ctx context.Context, req TransferRequest) (*TransferResponse, error) {
	token, err := c.GetToken(ctx)
	if err != nil {
		return nil, err
	}

	var resp TransferResponse
	path := fmt.Sprintf("/accounts/%s/transfers", c.accountID)
	if err := c.doJSON(ctx, http.MethodPost, path, token, req, &resp); err != nil {
		return nil, fmt.Errorf("nomba: transfer: %w", err)
	}
	if !resp.RequestSuccessful {
		return nil, &APIError{
			ResponseCode:    resp.ResponseCode,
			ResponseMessage: resp.ResponseMessage,
		}
	}
	return &resp, nil
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

// doJSON performs an HTTP request, serialising reqBody as JSON (when non-nil)
// and deserialising the response into respBody.
func (c *Client) doJSON(ctx context.Context, method, path, token string, reqBody, respBody interface{}) error {
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
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	c.log.Debug("nomba: http request", zap.String("method", method), zap.String("path", path))

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http do: %w", err)
	}
	defer res.Body.Close()

	rawBody, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	if res.StatusCode >= 400 {
		var apiErr APIError
		_ = json.Unmarshal(rawBody, &apiErr)
		if apiErr.ResponseCode == "" {
			apiErr.ResponseCode = strconv.Itoa(res.StatusCode)
			apiErr.ResponseMessage = string(rawBody)
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
