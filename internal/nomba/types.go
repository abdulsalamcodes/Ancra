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
// Nomba wraps the token inside a "data" envelope and uses camelCase keys.
type TokenResponse struct {
	RequestSuccessful bool   `json:"requestSuccessful"`
	ResponseCode      string `json:"responseCode"`
	ResponseMessage   string `json:"responseMessage"`
	Data              struct {
		AccessToken string `json:"accessToken"`
		TokenType   string `json:"tokenType"`
		ExpiresIn   int    `json:"expiresIn"` // seconds
	} `json:"data"`
}

// ---------------------------------------------------------------------------
// Virtual accounts
// ---------------------------------------------------------------------------

// CreateVirtualAccountRequest is the payload sent to Nomba to provision a DVA.
type CreateVirtualAccountRequest struct {
	AccountName   string `json:"accountName"`
	AccountRef    string `json:"accountRef"`             // your internal reference
	CustomerEmail string `json:"customerEmail"`
	CustomerName  string `json:"customerName"`
	PhoneNumber   string `json:"phoneNumber,omitempty"`
	BVN           string `json:"bvn,omitempty"`
	NIN           string `json:"nin,omitempty"`
}

// CreateVirtualAccountResponse is returned after successful DVA creation.
type CreateVirtualAccountResponse struct {
	RequestSuccessful bool   `json:"requestSuccessful"`
	ResponseCode      string `json:"responseCode"`
	ResponseMessage   string `json:"responseMessage"`
	Data              struct {
		AccountID         string `json:"accountId"`
		AccountRef        string `json:"accountRef"`
		AccountName       string `json:"accountName"`
		AccountNumber     string `json:"accountNumber"`
		BankCode          string `json:"bankCode"`
		BankName          string `json:"bankName"`
		CustomerEmail     string `json:"customerEmail"`
		CustomerName      string `json:"customerName"`
		Status            string `json:"status"`
	} `json:"data"`
}

// ---------------------------------------------------------------------------
// Wallet balance
// ---------------------------------------------------------------------------

// WalletBalanceResponse is returned by the wallet-balance endpoint.
type WalletBalanceResponse struct {
	RequestSuccessful bool   `json:"requestSuccessful"`
	ResponseCode      string `json:"responseCode"`
	ResponseMessage   string `json:"responseMessage"`
	Data              struct {
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
	RequestSuccessful bool   `json:"requestSuccessful"`
	ResponseCode      string `json:"responseCode"`
	ResponseMessage   string `json:"responseMessage"`
	Data              struct {
		Transactions []NombaTransaction `json:"transactions"`
		Page         int                `json:"page"`
		Limit        int                `json:"limit"`
		Total        int                `json:"total"`
	} `json:"data"`
}

// ---------------------------------------------------------------------------
// Transfer (outbound)
// ---------------------------------------------------------------------------

// TransferRequest is the payload to initiate a transfer out of the Nomba wallet.
type TransferRequest struct {
	Amount          int64  `json:"amount"`           // kobo
	Currency        string `json:"currency"`
	Narration       string `json:"narration"`
	Reference       string `json:"reference"`        // idempotency key
	DestinationBank string `json:"destinationBank"`
	DestinationAccount string `json:"destinationAccount"`
	DestinationName string `json:"destinationName"`
}

// TransferResponse is returned after initiating a transfer.
type TransferResponse struct {
	RequestSuccessful bool   `json:"requestSuccessful"`
	ResponseCode      string `json:"responseCode"`
	ResponseMessage   string `json:"responseMessage"`
	Data              struct {
		TransactionID string `json:"transactionId"`
		Reference     string `json:"reference"`
		Status        string `json:"status"`
	} `json:"data"`
}

// ---------------------------------------------------------------------------
// Webhooks
// ---------------------------------------------------------------------------

// WebhookPayload is the full event body Nomba POSTs to our endpoint.
type WebhookPayload struct {
	Event       string              `json:"event"`
	RequestID   string              `json:"requestId"`
	Transaction WebhookTransaction  `json:"transaction"`
	Customer    WebhookCustomer     `json:"customer"`
	Merchant    WebhookMerchant     `json:"merchant"`
}

// WebhookTransaction holds the financial details inside a webhook event.
type WebhookTransaction struct {
	TransactionID   string    `json:"transactionId"`
	AccountID       string    `json:"accountId"`
	Amount          float64   `json:"amount"`   // naira
	Fee             float64   `json:"fee"`
	Currency        string    `json:"currency"`
	Type            string    `json:"type"`
	Status          string    `json:"status"`
	Narration       string    `json:"narration"`
	Reference       string    `json:"reference"`
	BankCode        string    `json:"bankCode"`
	AccountNumber   string    `json:"accountNumber"`
	CreatedAt       time.Time `json:"createdAt"`
	SenderName      string    `json:"senderName,omitempty"`
	SenderBank      string    `json:"senderBank,omitempty"`
	SenderAccount   string    `json:"senderAccount,omitempty"`
}

// WebhookCustomer identifies the customer associated with the event.
type WebhookCustomer struct {
	CustomerID    string `json:"customerId"`
	CustomerEmail string `json:"customerEmail"`
	CustomerName  string `json:"customerName"`
	CustomerPhone string `json:"customerPhone,omitempty"`
}

// WebhookMerchant carries the merchant context (your Nomba account details).
type WebhookMerchant struct {
	AccountID   string `json:"accountId"`
	AccountName string `json:"accountName"`
	AccountRef  string `json:"accountRef"`
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

// APIError represents a structured error response from the Nomba API.
type APIError struct {
	ResponseCode    string `json:"responseCode"`
	ResponseMessage string `json:"responseMessage"`
}

func (e *APIError) Error() string {
	return "nomba: " + e.ResponseCode + " — " + e.ResponseMessage
}
