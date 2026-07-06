package nomba

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"net/http"
)

const signatureHeader = "X-Nomba-Signature"

// ErrInvalidSignature is returned when the HMAC-SHA512 digest on the incoming
// webhook request does not match what we compute with our secret.
var ErrInvalidSignature = errors.New("nomba webhook: invalid signature")

// Verifier checks the HMAC-SHA512 signature on inbound Nomba webhook requests.
type Verifier struct {
	secret []byte
}

// NewVerifier creates a Verifier using the shared webhook secret.
func NewVerifier(secret string) *Verifier {
	return &Verifier{secret: []byte(secret)}
}

// Verify reads the X-Nomba-Signature header from the request and compares it
// against an HMAC-SHA512 digest of body computed with the configured secret.
// body must be the raw, unmodified request body bytes.
//
// Returns nil on success, ErrInvalidSignature when the digest does not match,
// or another error if the header is missing.
func (v *Verifier) Verify(r *http.Request, body []byte) error {
	sig := r.Header.Get(signatureHeader)
	if sig == "" {
		return errors.New("nomba webhook: missing " + signatureHeader + " header")
	}

	mac := hmac.New(sha512.New, v.secret)
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return ErrInvalidSignature
	}

	return nil
}
