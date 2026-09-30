// Package enablebanking talks to the Enable Banking API (PSD2 account information).
// Docs: https://enablebanking.com/docs/api/reference/
package enablebanking

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Provider is the interface used by the sync job. Client (real) and Demo implement it.
type Provider interface {
	ASPSPs(ctx context.Context, country string) ([]ASPSP, error)
	StartAuth(ctx context.Context, req AuthRequest) (string, error)
	CreateSession(ctx context.Context, code string) (*Session, error)
	DeleteSession(ctx context.Context, sessionID string) error
	Balances(ctx context.Context, accountUID string) ([]Balance, error)
	Transactions(ctx context.Context, accountUID string, from, to time.Time, continuationKey string) (*TransactionPage, error)
}

type ASPSP struct {
	Name                   string   `json:"name"`
	Country                string   `json:"country"`
	Logo                   string   `json:"logo"`
	BIC                    string   `json:"bic"`
	Beta                   bool     `json:"beta"`
	MaximumConsentValidity int64    `json:"maximum_consent_validity"` // seconds
	PSUTypes               []string `json:"psu_types"`
}

type AuthRequest struct {
	ASPSPName    string
	ASPSPCountry string
	State        string
	RedirectURL  string
	ValidUntil   time.Time
}

type Amount struct {
	Currency string `json:"currency"`
	Amount   string `json:"amount"`
}

type AccountID struct {
	IBAN  string `json:"iban"`
	Other *struct {
		Identification string `json:"identification"`
	} `json:"other,omitempty"`
}

type Account struct {
	UID       string    `json:"uid"`
	AccountID AccountID `json:"account_id"`
	Name      string    `json:"name"`
	Currency  string    `json:"currency"`
	Product   string    `json:"product"`
	Details   string    `json:"details"`
	CashType  string    `json:"cash_account_type"`
}

type Session struct {
	SessionID string    `json:"session_id"`
	Accounts  []Account `json:"accounts"`
	ASPSP     struct {
		Name    string `json:"name"`
		Country string `json:"country"`
	} `json:"aspsp"`
	Access struct {
		ValidUntil time.Time `json:"valid_until"`
	} `json:"access"`
}

type Balance struct {
	Name          string `json:"name"`
	BalanceAmount Amount `json:"balance_amount"`
	BalanceType   string `json:"balance_type"`
	ReferenceDate string `json:"reference_date"`
	LastChange    string `json:"last_change_date_time"`
}

type Party struct {
	Name string `json:"name"`
}

type Transaction struct {
	TransactionID        string     `json:"transaction_id"`
	EntryReference       string     `json:"entry_reference"`
	BookingDate          string     `json:"booking_date"`
	ValueDate            string     `json:"value_date"`
	TransactionDate      string     `json:"transaction_date"`
	Status               string     `json:"status"`
	CreditDebitIndicator string     `json:"credit_debit_indicator"` // CRDT | DBIT
	TransactionAmount    Amount     `json:"transaction_amount"`
	Creditor             *Party     `json:"creditor"`
	Debtor               *Party     `json:"debtor"`
	CreditorAccount      *AccountID `json:"creditor_account"`
	DebtorAccount        *AccountID `json:"debtor_account"`
	BankTransactionCode  *struct {
		Code        string `json:"code"`
		SubCode     string `json:"sub_code"`
		Description string `json:"description"`
	} `json:"bank_transaction_code"`
	RemittanceInformation []string `json:"remittance_information"`
	Note                  string   `json:"note"`
}

type TransactionPage struct {
	Transactions    []Transaction `json:"transactions"`
	ContinuationKey string        `json:"continuation_key"`
}

// APIError is an error response from the API.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	b := e.Body
	if len(b) > 300 {
		b = b[:300] + "…"
	}
	return fmt.Sprintf("Enable Banking HTTP %d: %s", e.Status, b)
}

// SessionInvalid reports whether the bank consent has expired or been revoked
// and the user must reconnect the account.
func SessionInvalid(err error) bool {
	var ae *APIError
	if !errors.As(err, &ae) {
		return false
	}
	body := strings.ToUpper(ae.Body)
	return ae.Status == 401 || strings.Contains(body, "EXPIRED") || strings.Contains(body, "REVOKED") ||
		strings.Contains(body, "CLOSED_SESSION") || strings.Contains(body, "INVALID_SESSION")
}

// RateLimited reports whether the bank or the API has hit the request limit.
func RateLimited(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && (ae.Status == 429 || strings.Contains(strings.ToUpper(ae.Body), "ASPSP_RATE_LIMIT"))
}

// Client is the real Enable Banking client.
type Client struct {
	base  string
	appID string
	key   *rsa.PrivateKey
	http  *http.Client

	mu     sync.Mutex
	token  string
	expiry time.Time
}

func NewClient(base, appID, keyPath string) (*Client, error) {
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("privaten Schlüssel lesen: %w", err)
	}
	key, err := parseKey(raw)
	if err != nil {
		return nil, err
	}
	return &Client{base: base, appID: appID, key: key, http: &http.Client{Timeout: 60 * time.Second}}, nil
}

func parseKey(raw []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("privater Schlüssel ist kein PEM")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("privaten Schlüssel parsen: %w", err)
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("privater Schlüssel ist kein RSA-Schlüssel")
	}
	return rk, nil
}

// jwt creates (and caches) the RS256 token that authenticates each request.
func (c *Client) jwt() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if c.token != "" && now.Before(c.expiry.Add(-5*time.Minute)) {
		return c.token, nil
	}
	exp := now.Add(1 * time.Hour)
	tok, err := signJWT(c.key, c.appID, now, exp)
	if err != nil {
		return "", err
	}
	c.token, c.expiry = tok, exp
	return tok, nil
}

func signJWT(key *rsa.PrivateKey, kid string, iat, exp time.Time) (string, error) {
	enc := base64.RawURLEncoding
	h, _ := json.Marshal(map[string]string{"typ": "JWT", "alg": "RS256", "kid": kid})
	p, _ := json.Marshal(map[string]any{"iss": "enablebanking.com", "aud": "api.enablebanking.com", "iat": iat.Unix(), "exp": exp.Unix()})
	signing := enc.EncodeToString(h) + "." + enc.EncodeToString(p)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signing + "." + enc.EncodeToString(sig), nil
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	tok, err := c.jwt()
	if err != nil {
		return err
	}
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")
	if psu, ok := PSUFrom(ctx); ok && strings.HasPrefix(path, "/accounts/") {
		for k, v := range psu.headers() {
			req.Header.Set(k, v)
		}
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if resp.StatusCode >= 300 {
		return &APIError{Status: resp.StatusCode, Body: string(data)}
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("Antwort von %s nicht lesbar: %w", path, err)
	}
	return nil
}

func (c *Client) ASPSPs(ctx context.Context, country string) ([]ASPSP, error) {
	var out struct {
		ASPSPs []ASPSP `json:"aspsps"`
	}
	q := url.Values{"psu_type": {"personal"}, "service": {"AIS"}}
	if country != "" {
		q.Set("country", country)
	}
	err := c.do(ctx, http.MethodGet, "/aspsps", q, nil, &out)
	return out.ASPSPs, err
}

func (c *Client) StartAuth(ctx context.Context, r AuthRequest) (string, error) {
	body := map[string]any{
		"access":       map[string]string{"valid_until": r.ValidUntil.UTC().Format(time.RFC3339)},
		"aspsp":        map[string]string{"name": r.ASPSPName, "country": r.ASPSPCountry},
		"state":        r.State,
		"redirect_url": r.RedirectURL,
		"psu_type":     "personal",
		"language":     "de",
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := c.do(ctx, http.MethodPost, "/auth", nil, body, &out); err != nil {
		return "", err
	}
	return out.URL, nil
}

func (c *Client) CreateSession(ctx context.Context, code string) (*Session, error) {
	var s Session
	if err := c.do(ctx, http.MethodPost, "/sessions", nil, map[string]string{"code": code}, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (c *Client) DeleteSession(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/sessions/"+url.PathEscape(id), nil, nil, nil)
}

func (c *Client) Balances(ctx context.Context, uid string) ([]Balance, error) {
	var out struct {
		Balances []Balance `json:"balances"`
	}
	err := c.do(ctx, http.MethodGet, "/accounts/"+url.PathEscape(uid)+"/balances", nil, nil, &out)
	return out.Balances, err
}

func (c *Client) Transactions(ctx context.Context, uid string, from, to time.Time, cont string) (*TransactionPage, error) {
	q := url.Values{}
	if !from.IsZero() {
		q.Set("date_from", from.Format("2006-01-02"))
	}
	if !to.IsZero() {
		q.Set("date_to", to.Format("2006-01-02"))
	}
	if cont != "" {
		q.Set("continuation_key", cont)
	}
	var p TransactionPage
	err := c.do(ctx, http.MethodGet, "/accounts/"+url.PathEscape(uid)+"/transactions", q, nil, &p)
	return &p, err
}
