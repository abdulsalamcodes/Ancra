package nomba

import "time"

// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------

// TokenRequest is the body sent to the OAuth2 token endpoint.
type TokenRequest struct {
	GrantType    string `json:"grant_type"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// TokenResponse is returned by the OAuth2 token endpoint.
// Nomba envelope: {"code":"00","description":"Successful","data":{...}}
// Data keys are snake_case for the token endpoint specifically.
type TokenResponse struct {
	Code        string `json:"code"`
	Description string `json:"description"`
	Data        struct {
		AccessToken  string `json:"access_token"`
		BusinessID   string `json:"businessId"`
		RefreshToken string `json:"refresh_token"`
		ExpiresAt    string `json:"expiresAt"` // ISO8601 timestamp, e.g. "2026-07-07T05:42:45.029Z"
	} `json:"data"`
}

// ---------------------------------------------------------------------------
// Virtual accounts
// ---------------------------------------------------------------------------

// CreateVirtualAccountRequest is the payload sent to Nomba to provision a DVA.
// Endpoint: POST /accounts/virtual — accountId header must be the parent account ID.
// Ref: https://developer.nomba.com/docs/products/accept-payment/virtual-account
type CreateVirtualAccountRequest struct {
	AccountName    string  `json:"accountName"`
	AccountRef     string  `json:"accountRef"`              // your unique internal reference
	Currency       string  `json:"currency"`                // required: "NGN"
	BVN            string  `json:"bvn,omitempty"`
	ExpiryDate     string  `json:"expiryDate,omitempty"`    // for dynamic/one-time accounts
	ExpectedAmount float64 `json:"expectedAmount,omitempty"` // restrict to exact amount
}

// CreateVirtualAccountResponse is returned after successful DVA creation.
type CreateVirtualAccountResponse struct {
	Code        string `json:"code"`
	Description string `json:"description"`
	Data        struct {
		BankAccountNumber string `json:"bankAccountNumber"` // the assigned virtual account number
		AccountRef        string `json:"accountRef"`
		AccountName       string `json:"accountName"`
		Currency          string `json:"currency"`
		ExpiryDate        string `json:"expiryDate,omitempty"`
		Expired           bool   `json:"expired"`
	} `json:"data"`
}

// ---------------------------------------------------------------------------
// Wallet balance
// ---------------------------------------------------------------------------

// WalletBalanceResponse is returned by the wallet-balance endpoint.
type WalletBalanceResponse struct {
	Code        string `json:"code"`
	Description string `json:"description"`
	Data        struct {
		AccountID      string  `json:"accountId"`
		AvailableFloat float64 `json:"availableFloat"` // in naira; convert × 100 for kobo
		LedgerBalance  float64 `json:"ledgerBalance"`
		Currency       string  `json:"currency"`
	} `json:"data"`
}

// ---------------------------------------------------------------------------
// Transactions
// ---------------------------------------------------------------------------

// ListTransactionsRequest captures the query params for the transactions list.
type ListTransactionsRequest struct {
	AccountID string
	StartDate time.Time
	EndDate   time.Time
	Page      int
	Limit     int
}

// NombaTransaction represents a single transaction record from Nomba.
type NombaTransaction struct {
	TransactionID   string    `json:"transactionId"`
	AccountID       string    `json:"accountId"`
	Amount          float64   `json:"amount"`   // naira; × 100 = kobo
	Fee             float64   `json:"fee"`
	Currency        string    `json:"currency"`
	Type            string    `json:"type"`            // CREDIT / DEBIT
	Status          string    `json:"status"`          // SUCCESSFUL / FAILED / PENDING
	Narration       string    `json:"narration"`
	Reference       string    `json:"reference"`
	CreatedAt       time.Time `json:"createdAt"`
	SenderName      string    `json:"senderName,omitempty"`
	SenderBank      string    `json:"senderBank,omitempty"`
	RecipientName   string    `json:"recipientName,omitempty"`
	RecipientBank   string    `json:"recipientBank,omitempty"`
	RecipientNumber string    `json:"recipientNumber,omitempty"`
}

// ListTransactionsResponse wraps the paginated transaction list.
type ListTransactionsResponse struct {
	Code        string `json:"code"`
	Description string `json:"description"`
	Data        struct {
		Transactions []NombaTransaction `json:"transactions"`
		Page         int                `json:"page"`
		Limit        int                `json:"limit"`
		Total        int                `json:"total"`
	} `json:"data"`
}

// ---------------------------------------------------------------------------
// Transfer (outbound)
// ---------------------------------------------------------------------------

// TransferRequest is the payload for POST /v2/transfers/bank.
// Amount is in NGN (naira), not kobo.
// Ref: https://developer.nomba.com/docs/products/transfers/introduction
type TransferRequest struct {
	Amount        float64 `json:"amount"`        // NGN (naira)
	AccountNumber string  `json:"accountNumber"` // destination account number
	AccountName   string  `json:"accountName"`   // destination account name
	BankCode      string  `json:"bankCode"`
	MerchantTxRef string  `json:"merchantTxRef"` // idempotency key
	SenderName    string  `json:"senderName"`
}

// TransferResponse is returned by POST /v2/transfers/bank.
type TransferResponse struct {
	Code        string `json:"code"`
	Description string `json:"description"`
	Data        struct {
		ID          string  `json:"id"`
		Amount      float64 `json:"amount"`
		Fee         float64 `json:"fee"`
		Type        string  `json:"type"`
		Status      string  `json:"status"`
		TimeCreated string  `json:"timeCreated"`
		Meta        struct {
			MerchantTxRef string `json:"merchantTxRef"`
			RRN           string `json:"rrn"`
		} `json:"meta"`
	} `json:"data"`
}

// ---------------------------------------------------------------------------
// Bank lookup & bank list
// ---------------------------------------------------------------------------

// BankLookupRequest is the payload for POST /v1/transfers/bank/lookup.
type BankLookupRequest struct {
	AccountNumber string `json:"accountNumber"`
	BankCode      string `json:"bankCode"`
}

// BankLookupResponse is returned by the bank account lookup endpoint.
type BankLookupResponse struct {
	Code        string `json:"code"`
	Description string `json:"description"`
	Data        struct {
		AccountNumber string `json:"accountNumber"`
		AccountName   string `json:"accountName"`
	} `json:"data"`
}

// Bank represents a single entry in the bank directory.
type Bank struct {
	Name    string `json:"name"`
	Code    string `json:"code"`
	NipCode string `json:"nipCode"`
	Logo    string `json:"logo"`
}

// BankListResponse is returned by GET /v1/transfers/bank.
type BankListResponse struct {
	Code        string `json:"code"`
	Description string `json:"description"`
	Data        []Bank `json:"data"`
}

// ---------------------------------------------------------------------------
// Webhooks
// ---------------------------------------------------------------------------

// WebhookPayload is the full event body Nomba POSTs to our endpoint.
// Ref: https://developer.nomba.com/docs/api-basics/webhook#webhooks
type WebhookPayload struct {
	EventType string      `json:"event_type"`
	RequestID string      `json:"requestId"`
	Data      WebhookData `json:"data"`
}

// WebhookData wraps the nested merchant, terminal, transaction and customer
// objects inside the Nomba webhook payload.
type WebhookData struct {
	Merchant    WebhookMerchant    `json:"merchant"`
	Transaction WebhookTransaction `json:"transaction"`
	Customer    WebhookCustomer    `json:"customer"`
}

// WebhookMerchant carries your Nomba account identifiers.
type WebhookMerchant struct {
	UserID        string  `json:"userId"`
	WalletID      string  `json:"walletId"`
	WalletBalance float64 `json:"walletBalance"`
}

// WebhookTransaction holds the financial details inside a webhook event.
// For virtual-account credits, the destination is identified by AliasAccountNumber.
// TransactionAmount is in NGN; multiply by 100 to get kobo.
type WebhookTransaction struct {
	TransactionID         string  `json:"transactionId"`
	AliasAccountNumber    string  `json:"aliasAccountNumber"`    // the virtual account that received funds
	AliasAccountName      string  `json:"aliasAccountName"`
	AliasAccountReference string  `json:"aliasAccountReference"`
	AliasAccountType      string  `json:"aliasAccountType"`
	TransactionAmount     float64 `json:"transactionAmount"` // NGN; not kobo
	Fee                   float64 `json:"fee"`
	SessionID             string  `json:"sessionId"`
	Type                  string  `json:"type"`          // e.g. "vact_transfer"
	ResponseCode          string  `json:"responseCode"`
	OriginatingFrom       string  `json:"originatingFrom"`
	Narration             string  `json:"narration"`
	Time                  string  `json:"time"` // RFC3339
}

// WebhookCustomer identifies the sender on the incoming payment.
type WebhookCustomer struct {
	BankCode      string `json:"bankCode"`
	SenderName    string `json:"senderName"`
	BankName      string `json:"bankName"`
	AccountNumber string `json:"accountNumber"` // sender's bank account, not the virtual account
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

// APIError represents a structured error response from the Nomba API.
// Nomba error envelope: {"code":"404","description":"Resource not found","status":false}
type APIError struct {
	Code        string `json:"code"`
	Description string `json:"description"`
}

func (e *APIError) Error() string {
	return "nomba: " + e.Code + " — " + e.Description
}
