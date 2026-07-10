// Package integration_test contains end-to-end tests for the Ancra HTTP API.
// Each test spins up the real router wired to in-memory fake stores and a fake
// Nomba HTTP server — no real database or Nomba credentials required.
package integration_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/abdulsalamcodes/ancra/internal/api"
	"github.com/abdulsalamcodes/ancra/internal/crypto"
	"github.com/abdulsalamcodes/ancra/internal/domain/account"
	"github.com/abdulsalamcodes/ancra/internal/domain/ledger"
	"github.com/abdulsalamcodes/ancra/internal/domain/reconciliation"
	"github.com/abdulsalamcodes/ancra/internal/nomba"
	"github.com/abdulsalamcodes/ancra/internal/store"
	pgstore "github.com/abdulsalamcodes/ancra/internal/store/postgres"
)

const (
	testStaticKey     = "test-api-key"
	testAdminSecret   = "test-admin-secret"
	testWebhookSecret = "test-webhook-secret"
	testSubAccountID  = "sub-test-001"
	testAccountID     = "acct-test-001"
	testOrgID         = "00000000-0000-0000-0000-000000000001"
)

// ---------------------------------------------------------------------------
// Test environment
// ---------------------------------------------------------------------------

// testEnv holds the live test HTTP server and the in-memory stores so tests
// can inspect state directly without going through HTTP.
type testEnv struct {
	server  *httptest.Server
	nomba   *httptest.Server
	stores  *fakeStores
	log     *zap.Logger
}

// newTestEnv builds a fully wired test environment. The caller must call
// env.Close() at the end of the test (or use t.Cleanup).
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	log := zap.NewNop()

	fs := newFakeStores()

	nServer := newFakeNombaServer()

	nombaClient := nomba.NewClient(
		nServer.URL,
		"client-id",
		"client-secret",
		testAccountID,
		testSubAccountID,
		log,
	)

	// Build a ClientFactory for the reconciliation service. It wraps the same
	// fake Nomba server so reconciliation tests hit the in-memory HTTP stub.
	enc := mustTestEncryptor(t)
	nombaConfigs := newFakeNombaConfigStore(t, enc, nServer.URL)
	factory := nomba.NewClientFactory(nombaConfigs, enc, nServer.URL, log)

	acctSvc := account.NewService(fs.accounts, fs.customers, fs.ledger, factory, log)
	ledgerSvc := ledger.NewService(fs.ledger, log)
	reconSvc := reconciliation.NewService(fs.ledger, fs.recon, fs.accounts, fs.events, factory, log)

	verifier := nomba.NewVerifier(testWebhookSecret)

	router := api.NewRouter(api.RouterDeps{
		AccountSvc:     acctSvc,
		LedgerSvc:      ledgerSvc,
		ReconSvc:       reconSvc,
		NombaClient:    nombaClient,
		NombaFactory:   factory,
		Verifier:       verifier,
		Accounts:       fs.accounts,
		Orgs:           fs.orgs,
		Customers:      fs.customers,
		Events:         fs.events,
		Webhooks:       fs.webhooks,
		APIKeys:        fs.apiKeys,
		NombaConfigs:   nombaConfigs,
		Transactor:     noOpTransactor{},
		StaticKey:      testStaticKey,
		StaticKeyOrgID: testOrgID,
		AdminSecret:    testAdminSecret,
		Log:            log,
	})

	srv := httptest.NewServer(router)
	t.Cleanup(func() {
		srv.Close()
		nServer.Close()
	})

	return &testEnv{
		server: srv,
		nomba:  nServer,
		stores: fs,
		log:    log,
	}
}

// do performs an HTTP request against the test server and returns the response.
func (e *testEnv) do(t *testing.T, method, path string, body interface{}, headers map[string]string) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, e.server.URL+path, bodyReader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// authed adds the test Bearer token to the header map.
func authed(extra ...map[string]string) map[string]string {
	h := map[string]string{"Authorization": "Bearer " + testStaticKey}
	for _, m := range extra {
		for k, v := range m {
			h[k] = v
		}
	}
	return h
}

// admin adds the test Admin-Secret header.
func admin() map[string]string {
	return map[string]string{"Admin-Secret": testAdminSecret}
}

// decodeJSON decodes the response body into v and closes the body.
func decodeJSON(t *testing.T, resp *http.Response, v interface{}) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
}

// mustStatus asserts the response has the expected status code.
func mustStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("want status %d, got %d — body: %s", want, resp.StatusCode, body)
	}
}

// signWebhook computes the Nomba HMAC-SHA256 webhook signature.
// Nomba signs a colon-separated string of payload fields, not the raw body.
// Ref: https://developer.nomba.com/docs/api-basics/webhook#webhooks
func signWebhook(body []byte, timestamp, secret string) string {
	var p struct {
		EventType string `json:"event_type"`
		RequestID string `json:"requestId"`
		Data      struct {
			Merchant struct {
				UserID   string `json:"userId"`
				WalletID string `json:"walletId"`
			} `json:"merchant"`
			Transaction struct {
				TransactionID string `json:"transactionId"`
				Type          string `json:"type"`
				Time          string `json:"time"`
				ResponseCode  string `json:"responseCode"`
			} `json:"transaction"`
		} `json:"data"`
	}
	json.Unmarshal(body, &p) //nolint:errcheck

	responseCode := p.Data.Transaction.ResponseCode
	if responseCode == "null" {
		responseCode = ""
	}

	hashStr := fmt.Sprintf("%s:%s:%s:%s:%s:%s:%s:%s:%s",
		p.EventType, p.RequestID,
		p.Data.Merchant.UserID, p.Data.Merchant.WalletID,
		p.Data.Transaction.TransactionID, p.Data.Transaction.Type,
		p.Data.Transaction.Time, responseCode,
		timestamp,
	)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(hashStr))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// webhookBody constructs and signs a payment_success payload for the given
// virtual account number and amount (in naira). Returns the body, HMAC
// signature, and nomba-timestamp header value — all three are required to call
// postWebhook.
func webhookBody(t *testing.T, txnID, accountNumber string, amountNaira float64) (body []byte, sig, timestamp string) {
	t.Helper()
	timestamp = time.Now().UTC().Format(time.RFC3339)
	payload := map[string]interface{}{
		"event_type": "payment_success",
		"requestId":  uuid.New().String(),
		"data": map[string]interface{}{
			"merchant": map[string]interface{}{
				"userId":        "test-user-id",
				"walletId":      "test-wallet-id",
				"walletBalance": amountNaira,
			},
			"terminal": map[string]interface{}{},
			"transaction": map[string]interface{}{
				"transactionId":      txnID,
				"aliasAccountNumber": accountNumber,
				"aliasAccountName":   "Test Account",
				"transactionAmount":  amountNaira,
				"fee":                0,
				"sessionId":          "test-session-" + txnID,
				"type":               "vact_transfer",
				"responseCode":       "",
				"originatingFrom":    "api",
				"narration":          "test transfer",
				"time":               timestamp,
				"aliasAccountType":   "VIRTUAL",
			},
			"customer": map[string]interface{}{
				"bankCode":      "044",
				"senderName":    "John Doe",
				"bankName":      "Access Bank",
				"accountNumber": "0123456789",
			},
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal webhook: %v", err)
	}
	return b, signWebhook(b, timestamp, testWebhookSecret), timestamp
}

// ---------------------------------------------------------------------------
// Fake Nomba HTTP server
// ---------------------------------------------------------------------------

func newFakeNombaServer() *httptest.Server {
	mux := http.NewServeMux()

	// OAuth2 token — matches real Nomba envelope format
	mux.HandleFunc("/v1/auth/token/issue", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"code":        "00",
			"description": "Successful",
			"data": map[string]interface{}{
				"access_token": "fake-nomba-token",
				"expiresAt":    "2099-01-01T00:00:00.000Z",
				"businessId":   testAccountID,
			},
		})
	})

	// Create virtual account: POST /v1/accounts/virtual
	mux.HandleFunc("/v1/accounts/virtual", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req nomba.CreateVirtualAccountRequest
		json.NewDecoder(r.Body).Decode(&req) //nolint:errcheck
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"code":        "00",
			"description": "Successful",
			"data": map[string]interface{}{
				"bankAccountNumber": "1234567890",
				"accountRef":        req.AccountRef,
				"accountName":       req.AccountName,
				"currency":          "NGN",
				"expired":           false,
			},
		})
	})

	// Bank transfer: POST /v2/transfers/bank
	mux.HandleFunc("/v2/transfers/bank", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"code":        "00",
			"description": "Success",
			"data": map[string]interface{}{
				"id":          "API-TRANSFER-" + uuid.New().String(),
				"amount":      100,
				"fee":         50,
				"type":        "transfer",
				"status":      "SUCCESS",
				"timeCreated": time.Now().UTC().Format(time.RFC3339),
				"meta": map[string]interface{}{
					"merchantTxRef": "",
					"rrn":           "",
				},
			},
		})
	})

	// Bank account lookup: POST /v1/transfers/bank/lookup
	mux.HandleFunc("/v1/transfers/bank/lookup", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"code":        "00",
			"description": "Success",
			"data": map[string]interface{}{
				"accountNumber": "0123456789",
				"accountName":   "Test Recipient",
			},
		})
	})

	// Bank list: GET /v1/transfers/bank
	mux.HandleFunc("/v1/transfers/bank", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"code":        "00",
			"description": "Success",
			"data": []map[string]interface{}{
				{"name": "Access Bank", "code": "044", "nipCode": "044", "logo": ""},
				{"name": "First Bank", "code": "011", "nipCode": "011", "logo": ""},
			},
		})
	})

	// Catch-all for /v1/accounts/...
	mux.HandleFunc("/v1/accounts/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {

		// Wallet balance: GET /v1/accounts/{subID}/balance
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/balance"):
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"code":        "00",
				"description": "Successful",
				"data": map[string]interface{}{
					"amount":   "0.0", // naira decimal string as Nomba returns it
					"currency": "NGN",
				},
			})

		// Transactions: GET /v1/accounts/{id}/transactions
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/transactions"):
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"code":        "00",
				"description": "Successful",
				"data": map[string]interface{}{
					"transactions": []interface{}{},
					"page":         1,
					"limit":        20,
					"total":        0,
				},
			})

		default:
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "not found"}) //nolint:errcheck
		}
	})

	return httptest.NewServer(mux)
}

// ---------------------------------------------------------------------------
// In-memory fake stores
// ---------------------------------------------------------------------------

type fakeStores struct {
	accounts  *fakeAccountStore
	customers *fakeCustomerStore
	ledger    *fakeLedgerStore
	events    *fakeEventStore
	webhooks  *fakeWebhookStore
	recon     *fakeReconStore
	apiKeys   *fakeAPIKeyStore
	orgs      *fakeOrgStore
}

func newFakeStores() *fakeStores {
	return &fakeStores{
		accounts: &fakeAccountStore{data: map[uuid.UUID]*store.VirtualAccount{}},
		customers: &fakeCustomerStore{
			customers:  map[uuid.UUID]*store.Customer{},
			identities: []*store.IdentityVersion{},
		},
		ledger:   newFakeLedgerStore(),
		events:   &fakeEventStore{seen: map[string]struct{}{}},
		webhooks: &fakeWebhookStore{data: []*store.WebhookDelivery{}},
		recon:    &fakeReconStore{runs: []*store.ReconciliationRun{}},
		apiKeys:  &fakeAPIKeyStore{data: map[uuid.UUID]*store.APIKey{}},
		orgs:     &fakeOrgStore{data: map[uuid.UUID]*store.Organization{}},
	}
}

// --- fakeAccountStore ---

type fakeAccountStore struct {
	mu   sync.RWMutex
	data map[uuid.UUID]*store.VirtualAccount
}

func (s *fakeAccountStore) CreateAccount(_ context.Context, a *store.VirtualAccount) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a.OrgID == (uuid.UUID{}) {
		a.OrgID = uuid.MustParse(testOrgID)
	}
	s.data[a.ID] = a
	return nil
}

func (s *fakeAccountStore) GetAccount(_ context.Context, _ uuid.UUID, id uuid.UUID) (*store.VirtualAccount, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.data[id]
	if !ok {
		return nil, errors.New("account not found")
	}
	return a, nil
}

func (s *fakeAccountStore) GetAccountByNumber(_ context.Context, number string) (*store.VirtualAccount, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.data {
		if a.BankAccountNumber == number {
			return a, nil
		}
	}
	return nil, errors.New("account not found")
}

func (s *fakeAccountStore) ListAccounts(_ context.Context, _ uuid.UUID, limit, offset int) ([]*store.VirtualAccount, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var all []*store.VirtualAccount
	for _, a := range s.data {
		all = append(all, a)
	}
	if offset >= len(all) {
		return []*store.VirtualAccount{}, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], nil
}

func (s *fakeAccountStore) ListAccountsByCustomer(_ context.Context, _ uuid.UUID, customerID uuid.UUID) ([]*store.VirtualAccount, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*store.VirtualAccount
	for _, a := range s.data {
		if a.CustomerID == customerID {
			out = append(out, a)
		}
	}
	return out, nil
}

func (s *fakeAccountStore) UpdateAccountStatus(_ context.Context, id uuid.UUID, status store.AccountStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.data[id]
	if !ok {
		return errors.New("account not found")
	}
	a.Status = status
	return nil
}

// --- fakeCustomerStore ---

type fakeCustomerStore struct {
	mu         sync.RWMutex
	customers  map[uuid.UUID]*store.Customer
	identities []*store.IdentityVersion
	kycHistory []*store.KYCTierChange
}

func (s *fakeCustomerStore) CreateCustomer(_ context.Context, c *store.Customer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Ensure the test org is always set so org-scoped lookups succeed.
	if c.OrgID == (uuid.UUID{}) {
		c.OrgID = uuid.MustParse(testOrgID)
	}
	s.customers[c.ID] = c
	return nil
}

func (s *fakeCustomerStore) GetCustomer(_ context.Context, _ uuid.UUID, id uuid.UUID) (*store.Customer, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.customers[id]
	if !ok {
		return nil, errors.New("customer not found")
	}
	return c, nil
}

func (s *fakeCustomerStore) ListCustomers(_ context.Context, _ uuid.UUID, limit, offset int) ([]*store.Customer, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var all []*store.Customer
	for _, c := range s.customers {
		all = append(all, c)
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].CreatedAt.Before(all[j].CreatedAt)
	})
	if offset >= len(all) {
		return []*store.Customer{}, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], nil
}

func (s *fakeCustomerStore) CreateIdentityVersion(_ context.Context, v *store.IdentityVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.identities = append(s.identities, v)
	return nil
}

func (s *fakeCustomerStore) GetCurrentIdentity(_ context.Context, customerID uuid.UUID) (*store.IdentityVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := len(s.identities) - 1; i >= 0; i-- {
		v := s.identities[i]
		if v.CustomerID == customerID && v.EffectiveTo == nil {
			return v, nil
		}
	}
	return nil, errors.New("no current identity")
}

func (s *fakeCustomerStore) CloseIdentityVersion(_ context.Context, id uuid.UUID, closedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.identities {
		if v.ID == id {
			v.EffectiveTo = &closedAt
			return nil
		}
	}
	return errors.New("identity version not found")
}

func (s *fakeCustomerStore) UpgradeKYCTier(_ context.Context, _ uuid.UUID, customerID uuid.UUID, newTier int, now time.Time) (*store.KYCTierChange, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.customers[customerID]
	if !ok {
		return nil, errors.New("customer not found")
	}
	if newTier <= c.KYCTier {
		return nil, store.ErrKYCTierDowngrade
	}
	change := &store.KYCTierChange{
		ID:         uuid.New(),
		CustomerID: customerID,
		FromTier:   c.KYCTier,
		ToTier:     newTier,
		UpgradedAt: now,
	}
	c.KYCTier = newTier
	s.kycHistory = append(s.kycHistory, change)
	return change, nil
}

func (s *fakeCustomerStore) ListKYCTierHistory(_ context.Context, _ uuid.UUID, customerID uuid.UUID) ([]*store.KYCTierChange, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*store.KYCTierChange
	for _, ch := range s.kycHistory {
		if ch.CustomerID == customerID {
			out = append(out, ch)
		}
	}
	if out == nil {
		out = []*store.KYCTierChange{}
	}
	return out, nil
}

// --- fakeLedgerStore ---

type fakeLedgerStore struct {
	mu             sync.RWMutex
	entries        []*store.LedgerEntry
	systemAccounts map[string]*store.SystemAccount
}

func newFakeLedgerStore() *fakeLedgerStore {
	sysAccounts := map[string]*store.SystemAccount{
		"pool":              {ID: uuid.New(), Name: "pool"},
		"suspense":          {ID: uuid.New(), Name: "suspense"},
		"fees":              {ID: uuid.New(), Name: "fees"},
		"returns_clearing":  {ID: uuid.New(), Name: "returns_clearing"},
	}
	return &fakeLedgerStore{
		entries:        []*store.LedgerEntry{},
		systemAccounts: sysAccounts,
	}
}

func (s *fakeLedgerStore) InsertEntries(_ context.Context, entries []*store.LedgerEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, entries...)
	return nil
}

func (s *fakeLedgerStore) GetBalance(_ context.Context, accountID uuid.UUID) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var balance int64
	for _, e := range s.entries {
		if e.AccountID != accountID {
			continue
		}
		if e.Direction == store.DirectionCredit {
			balance += e.Amount
		} else {
			balance -= e.Amount
		}
	}
	return balance, nil
}

func (s *fakeLedgerStore) GetBalanceAsOf(_ context.Context, accountID uuid.UUID, asOf time.Time) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var balance int64
	for _, e := range s.entries {
		if e.AccountID != accountID {
			continue
		}
		if e.CreatedAt.After(asOf) {
			continue
		}
		if e.Direction == store.DirectionCredit {
			balance += e.Amount
		} else {
			balance -= e.Amount
		}
	}
	return balance, nil
}

func (s *fakeLedgerStore) ListEntries(_ context.Context, accountID uuid.UUID, limit, offset int) ([]*store.LedgerEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var matching []*store.LedgerEntry
	for _, e := range s.entries {
		if e.AccountID == accountID {
			matching = append(matching, e)
		}
	}
	// Order newest first
	sort.Slice(matching, func(i, j int) bool {
		return matching[i].CreatedAt.After(matching[j].CreatedAt)
	})
	if offset >= len(matching) {
		return []*store.LedgerEntry{}, nil
	}
	end := offset + limit
	if end > len(matching) {
		end = len(matching)
	}
	return matching[offset:end], nil
}

func (s *fakeLedgerStore) GetSystemAccount(_ context.Context, _ uuid.UUID, name string) (*store.SystemAccount, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.systemAccounts[name]
	if !ok {
		return nil, errors.New("system account not found: " + name)
	}
	return a, nil
}

func (s *fakeLedgerStore) SeedSystemAccounts(_ context.Context, _ uuid.UUID) error {
	return nil
}

// --- fakeEventStore ---

type fakeEventStore struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

func (s *fakeEventStore) MarkProcessed(_ context.Context, e *store.ProcessedEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.seen[e.TransactionID]; exists {
		return pgstore.ErrAlreadyProcessed
	}
	s.seen[e.TransactionID] = struct{}{}
	return nil
}

func (s *fakeEventStore) IsProcessed(_ context.Context, transactionID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, exists := s.seen[transactionID]
	return exists, nil
}

// --- fakeWebhookStore ---

type fakeWebhookStore struct {
	mu   sync.RWMutex
	data []*store.WebhookDelivery
}

func (s *fakeWebhookStore) CreateDelivery(_ context.Context, d *store.WebhookDelivery) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = append(s.data, d)
	return nil
}

func (s *fakeWebhookStore) GetDelivery(_ context.Context, id uuid.UUID) (*store.WebhookDelivery, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, d := range s.data {
		if d.ID == id {
			return d, nil
		}
	}
	return nil, errors.New("delivery not found")
}

func (s *fakeWebhookStore) ListPending(_ context.Context, now time.Time, limit int) ([]*store.WebhookDelivery, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*store.WebhookDelivery
	for _, d := range s.data {
		if d.Status == store.WebhookStatusPending {
			out = append(out, d)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (s *fakeWebhookStore) ListDeliveries(_ context.Context, _ uuid.UUID, limit, offset int) ([]*store.WebhookDelivery, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if offset >= len(s.data) {
		return []*store.WebhookDelivery{}, nil
	}
	end := offset + limit
	if end > len(s.data) {
		end = len(s.data)
	}
	return s.data[offset:end], nil
}

func (s *fakeWebhookStore) ListAllDeliveries(_ context.Context, limit, offset int) ([]*store.WebhookDelivery, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if offset >= len(s.data) {
		return []*store.WebhookDelivery{}, nil
	}
	end := offset + limit
	if end > len(s.data) {
		end = len(s.data)
	}
	return s.data[offset:end], nil
}

func (s *fakeWebhookStore) UpdateDelivery(_ context.Context, d *store.WebhookDelivery) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.data {
		if existing.ID == d.ID {
			s.data[i] = d
			return nil
		}
	}
	return errors.New("delivery not found")
}

// --- fakeReconStore ---

type fakeReconStore struct {
	mu   sync.RWMutex
	runs []*store.ReconciliationRun
}

func (s *fakeReconStore) InsertRun(_ context.Context, run *store.ReconciliationRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs = append(s.runs, run)
	return nil
}

func (s *fakeReconStore) ListRuns(_ context.Context, _ uuid.UUID, limit, offset int) ([]*store.ReconciliationRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if offset >= len(s.runs) {
		return []*store.ReconciliationRun{}, nil
	}
	end := offset + limit
	if end > len(s.runs) {
		end = len(s.runs)
	}
	return s.runs[offset:end], nil
}

func (s *fakeReconStore) ListAllRuns(_ context.Context, limit, offset int) ([]*store.ReconciliationRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if offset >= len(s.runs) {
		return []*store.ReconciliationRun{}, nil
	}
	end := offset + limit
	if end > len(s.runs) {
		end = len(s.runs)
	}
	return s.runs[offset:end], nil
}

func (s *fakeReconStore) GetLatestRun(_ context.Context, _ uuid.UUID) (*store.ReconciliationRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.runs) == 0 {
		return nil, errors.New("no runs found")
	}
	return s.runs[len(s.runs)-1], nil
}

// --- fakeAPIKeyStore ---

type fakeAPIKeyStore struct {
	mu   sync.RWMutex
	data map[uuid.UUID]*store.APIKey
}

func (s *fakeAPIKeyStore) CreateKey(_ context.Context, k *store.APIKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[k.ID] = k
	return nil
}

func (s *fakeAPIKeyStore) GetByHash(_ context.Context, hash string) (*store.APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, k := range s.data {
		if k.KeyHash == hash && k.RevokedAt == nil {
			return k, nil
		}
	}
	return nil, errors.New("api key not found")
}

func (s *fakeAPIKeyStore) GetByID(_ context.Context, id uuid.UUID) (*store.APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	k, ok := s.data[id]
	if !ok {
		return nil, errors.New("api key not found")
	}
	return k, nil
}

func (s *fakeAPIKeyStore) ListKeys(_ context.Context, orgID uuid.UUID) ([]*store.APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*store.APIKey
	for _, k := range s.data {
		// uuid.Nil is the sentinel for "all orgs" (admin list endpoint).
		if orgID == (uuid.UUID{}) || (k.OrgID != nil && *k.OrgID == orgID) {
			out = append(out, k)
		}
	}
	return out, nil
}

func (s *fakeAPIKeyStore) ListAllKeys(_ context.Context) ([]*store.APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*store.APIKey, 0, len(s.data))
	for _, k := range s.data {
		out = append(out, k)
	}
	return out, nil
}

func (s *fakeAPIKeyStore) RevokeKey(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.data[id]
	if !ok {
		return errors.New("api key not found")
	}
	now := time.Now().UTC()
	k.RevokedAt = &now
	return nil
}

func (s *fakeAPIKeyStore) TouchLastUsed(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.data[id]
	if !ok {
		return errors.New("api key not found")
	}
	now := time.Now().UTC()
	k.LastUsedAt = &now
	return nil
}

// --- fakeOrgStore ---

type fakeOrgStore struct {
	mu   sync.RWMutex
	data map[uuid.UUID]*store.Organization
}

func (s *fakeOrgStore) CreateOrg(_ context.Context, org *store.Organization) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[org.ID] = org
	return nil
}

func (s *fakeOrgStore) GetOrgByID(_ context.Context, id uuid.UUID) (*store.Organization, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	org, ok := s.data[id]
	if !ok {
		return nil, errors.New("org not found")
	}
	return org, nil
}

func (s *fakeOrgStore) GetOrgBySlug(_ context.Context, slug string) (*store.Organization, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, org := range s.data {
		if org.Slug == slug {
			return org, nil
		}
	}
	return nil, errors.New("org not found")
}

func (s *fakeOrgStore) ListAllOrgs(_ context.Context) ([]*store.Organization, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*store.Organization, 0, len(s.data))
	for _, org := range s.data {
		out = append(out, org)
	}
	return out, nil
}

// --- noOpTransactor ---

// noOpTransactor satisfies store.Transactor for tests that use fake in-memory
// stores. It runs fn directly without a real DB transaction, which is correct
// because the fake stores don't require transactional isolation.
type noOpTransactor struct{}

func (noOpTransactor) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

// ---------------------------------------------------------------------------
// Shared setup helpers
// ---------------------------------------------------------------------------

// createCustomer POSTs to /customers and returns the created customer.
func createCustomer(t *testing.T, env *testEnv, kycTier int) map[string]interface{} {
	t.Helper()
	resp := env.do(t, http.MethodPost, "/customers",
		map[string]interface{}{"kyc_tier": kycTier},
		authed())
	mustStatus(t, resp, http.StatusCreated)
	var out map[string]interface{}
	decodeJSON(t, resp, &out)
	return out
}

// createAccount POSTs to /accounts for the given customer ID and returns the
// inner "Account" map (the VirtualAccount fields).
func createAccount(t *testing.T, env *testEnv, customerID string) map[string]interface{} {
	t.Helper()
	resp := env.do(t, http.MethodPost, "/accounts", map[string]interface{}{
		"customer_id":    customerID,
		"display_name":   "Test User",
		"customer_email": "test@example.com",
	}, authed())
	mustStatus(t, resp, http.StatusCreated)
	var out map[string]interface{}
	decodeJSON(t, resp, &out)
	// CreateAccountResponse serialises as {"account":{...},"identity":{...}}
	acct, ok := out["account"].(map[string]interface{})
	if !ok {
		t.Fatalf("createAccount: response missing 'account' field, got: %v", out)
	}
	return acct
}

// ---------------------------------------------------------------------------
// fakeNombaConfigStore — returns pre-configured credentials for any orgID
// so the reconciliation service can build a Nomba client via the factory.
// ---------------------------------------------------------------------------

type fakeNombaConfigStore struct {
	mu   sync.RWMutex
	data map[uuid.UUID]*store.OrgNombaConfig
}

func newFakeNombaConfigStore(t *testing.T, enc *crypto.Encryptor, _ string) *fakeNombaConfigStore {
	t.Helper()
	encSecret, err := enc.Encrypt("client-secret")
	if err != nil {
		t.Fatalf("encrypt client secret: %v", err)
	}
	encWebhook, err := enc.Encrypt(testWebhookSecret)
	if err != nil {
		t.Fatalf("encrypt webhook secret: %v", err)
	}
	orgID := uuid.MustParse(testOrgID)
	cfg := &store.OrgNombaConfig{
		OrgID:                  orgID,
		ClientID:               "client-id",
		ClientSecretEncrypted:  encSecret,
		AccountID:              testAccountID,
		SubAccountID:           testSubAccountID,
		WebhookSecretEncrypted: encWebhook,
		Sandbox:                false,
		CreatedAt:              time.Now().UTC(),
		UpdatedAt:              time.Now().UTC(),
	}
	s := &fakeNombaConfigStore{data: map[uuid.UUID]*store.OrgNombaConfig{}}
	s.data[orgID] = cfg
	return s
}

func (s *fakeNombaConfigStore) UpsertNombaConfig(_ context.Context, cfg *store.OrgNombaConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[cfg.OrgID] = cfg
	return nil
}

func (s *fakeNombaConfigStore) GetNombaConfig(_ context.Context, orgID uuid.UUID) (*store.OrgNombaConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cfg, ok := s.data[orgID]
	if !ok {
		return nil, errors.New("nomba config not found")
	}
	return cfg, nil
}

// mustTestEncryptor returns a crypto.Encryptor seeded with a fixed 32-byte
// test key. Fatals the test on construction failure.
func mustTestEncryptor(t *testing.T) *crypto.Encryptor {
	t.Helper()
	key := []byte("ancra-test-enc-key-32-bytes-long")
	enc, err := crypto.NewEncryptor(key)
	if err != nil {
		t.Fatalf("new encryptor: %v", err)
	}
	return enc
}

// ---------------------------------------------------------------------------

// postWebhook sends a signed webhook to /webhooks/nomba.
// sig and timestamp are the values returned by webhookBody.
func postWebhook(t *testing.T, env *testEnv, body []byte, sig, timestamp string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, env.server.URL+"/webhooks/nomba", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("nomba-signature", sig)
	req.Header.Set("nomba-timestamp", timestamp)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do webhook: %v", err)
	}
	return resp
}
