package enablebanking

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseCents(t *testing.T) {
	for in, want := range map[string]int64{"12.34": 1234, "12.3": 1230, "12": 1200, "-0.99": -99, "1234,56": 123456, ".5": 50} {
		got, err := ParseCents(in)
		if err != nil || got != want {
			t.Errorf("ParseCents(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
}

func TestJWTAndRequests(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		switch {
		case r.URL.Path == "/accounts/abc/transactions" && r.URL.Query().Get("continuation_key") == "":
			w.Write([]byte(`{"transactions":[{"entry_reference":"e1","booking_date":"2026-09-01","status":"BOOK","credit_debit_indicator":"DBIT","transaction_amount":{"currency":"EUR","amount":"13.99"},"creditor":{"name":"Netflix"},"remittance_information":["Abo"]}],"continuation_key":"k2"}`))
		case r.URL.Path == "/accounts/abc/transactions":
			w.Write([]byte(`{"transactions":[]}`))
		case r.URL.Path == "/accounts/expired/balances":
			w.WriteHeader(401)
			w.Write([]byte(`{"code":401,"message":"Session expired","error":"EXPIRED_SESSION"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := &Client{base: srv.URL, appID: "app-123", key: key, http: srv.Client()}

	p, err := c.Transactions(context.Background(), "abc", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), time.Time{}, "")
	if err != nil || len(p.Transactions) != 1 || p.ContinuationKey != "k2" {
		t.Fatalf("Transactions: %+v %v", p, err)
	}
	n, err := Normalize(p.Transactions[0], map[string]int{})
	if err != nil || n.AmountCents != -1399 || n.Counterparty != "Netflix" || n.ExtID != "e1" {
		t.Fatalf("Normalize: %+v %v", n, err)
	}

	// check JWT
	tok := strings.TrimPrefix(gotAuth, "Bearer ")
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("not a JWT: %q", gotAuth)
	}
	var hdr map[string]string
	hb, _ := base64.RawURLEncoding.DecodeString(parts[0])
	json.Unmarshal(hb, &hdr)
	if hdr["kid"] != "app-123" || hdr["alg"] != "RS256" {
		t.Errorf("Header: %v", hdr)
	}
	var claims map[string]any
	pb, _ := base64.RawURLEncoding.DecodeString(parts[1])
	json.Unmarshal(pb, &claims)
	if claims["aud"] != "api.enablebanking.com" || claims["iss"] != "enablebanking.com" {
		t.Errorf("Claims: %v", claims)
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
		t.Errorf("invalid signature: %v", err)
	}

	_, err = c.Balances(context.Background(), "expired")
	if !SessionInvalid(err) {
		t.Errorf("expired session not detected: %v", err)
	}
}

func TestDemoFlow(t *testing.T) {
	d := NewDemo()
	d.now = func() time.Time { return time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC) }
	u, _ := d.StartAuth(context.Background(), AuthRequest{ASPSPName: "Demo-Sparkasse", State: "s1", RedirectURL: "http://x/cb"})
	if !strings.Contains(u, "state=s1") {
		t.Fatal(u)
	}
	s, _ := d.CreateSession(context.Background(), "demo:Demo-Sparkasse")
	if len(s.Accounts) != 2 {
		t.Fatalf("accounts: %d", len(s.Accounts))
	}
	total := 0
	cont := ""
	for {
		p, _ := d.Transactions(context.Background(), "demo-joint", time.Time{}, time.Time{}, cont)
		total += len(p.Transactions)
		if p.ContinuationKey == "" {
			break
		}
		cont = p.ContinuationKey
	}
	if total < 300 {
		t.Errorf("too few demo transactions: %d", total)
	}
}
