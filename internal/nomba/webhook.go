package nomba

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

const (
	signatureHeader = "nomba-signature"
	timestampHeader = "nomba-timestamp"
)

// ErrInvalidSignature is returned when the HMAC-SHA256 digest on the incoming
// webhook request does not match what we compute with our secret.
var ErrInvalidSignature = errors.New("nomba webhook: invalid signature")

// Verifier checks the HMAC-SHA256 signature on inbound Nomba webhook requests.
type Verifier struct {
	secret []byte
}

// NewVerifier creates a Verifier using the shared webhook secret configured on
// the Nomba dashboard.
func NewVerifier(secret string) *Verifier {
	return &Verifier{secret: []byte(secret)}
}

// Verify validates the HMAC-SHA256 signature on a Nomba webhook request.
//
// Nomba does not sign the raw body. Instead it signs a colon-separated string
// constructed from specific payload fields plus the nomba-timestamp header:
//
//	event_type:request_id:user_id:wallet_id:transaction_id:transaction_type:transaction_time:response_code:timestamp
//
// Ref: https://developer.nomba.com/docs/api-basics/webhook#webhooks
func (v *Verifier) Verify(r *http.Request, body []byte) error {
	sig := r.Header.Get(signatureHeader)
	if sig == "" {
		return errors.New("nomba webhook: missing " + signatureHeader + " header")
	}

	timestamp := r.Header.Get(timestampHeader)

	hashingString, err := buildHashingString(body, timestamp)
	if err != nil {
		return fmt.Errorf("nomba webhook: build hashing string: %w", err)
	}

	mac := hmac.New(sha256.New, v.secret)
	mac.Write([]byte(hashingString))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return ErrInvalidSignature
	}

	return nil
}

// buildHashingString constructs the colon-separated string that Nomba signs.
// Format: event_type:request_id:user_id:wallet_id:transaction_id:transaction_type:transaction_time:response_code:timestamp
func buildHashingString(body []byte, timestamp string) (string, error) {
	var p WebhookPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return "", fmt.Errorf("parse payload: %w", err)
	}

	txn := p.Data.Transaction
	merchant := p.Data.Merchant

	responseCode := txn.ResponseCode
	if responseCode == "null" {
		responseCode = ""
	}

	return fmt.Sprintf("%s:%s:%s:%s:%s:%s:%s:%s:%s",
		p.EventType,
		p.RequestID,
		merchant.UserID,
		merchant.WalletID,
		txn.TransactionID,
		txn.Type,
		txn.Time,
		responseCode,
		timestamp,
	), nil
}
