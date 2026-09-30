package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/AQUI74S/homestead/internal/config"
	"github.com/AQUI74S/homestead/internal/domain"
	eb "github.com/AQUI74S/homestead/internal/enablebanking"
	"github.com/AQUI74S/homestead/internal/store"
	"github.com/AQUI74S/homestead/internal/syncer"
)

func TestHausverwaltungEndToEnd(t *testing.T) {
	dsn := os.Getenv("HS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("HS_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	st.DB.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public;`)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := eb.NewDemo()
	sy := syncer.New(st, p, log)
	cfg := config.Config{Demo: true, EBCountry: "DE", EBConsentMax: 180 * 24 * time.Hour}
	srv := httptest.NewServer(nil)
	defer srv.Close()
	cfg.PublicURL = srv.URL
	srv.Config.Handler = New(cfg, st, p, sy, log, fstest.MapFS{"index.html": {Data: []byte("x")}}).Handler()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	do := func(method, path string, body any, out any) int {
		t.Helper()
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = strings.NewReader(string(b))
		}
		req, _ := http.NewRequest(method, srv.URL+path, rd)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if out != nil {
			json.NewDecoder(resp.Body).Decode(out)
		}
		return resp.StatusCode
	}
	// Empty Hausverwaltung (property management): all lists must be lists, never null
	checkLists := func(path string, keys ...string) {
		t.Helper()
		var m map[string]any
		if code := do("GET", path, nil, &m); code != 200 {
			t.Fatalf("%s: %d", path, code)
		}
		for _, k := range keys {
			if _, ok := m[k].([]any); !ok {
				t.Errorf("%s: %s is %v instead of a list", path, k, m[k])
			}
		}
	}
	checkLists("/api/hv/overview", "properties", "leases", "deadlines", "accounts")
	checkLists("/api/hv/report", "properties", "deadlines", "reminders")
	checkLists("/api/hv/meta", "cost_types", "properties", "leases")
	var txl any
	do("GET", "/api/hv/transactions", nil, &txl)
	if _, ok := txl.([]any); !ok {
		t.Errorf("/api/hv/transactions: %v instead of a list", txl)
	}

	for bank, book := range map[string]string{"Demo-Sparkasse": "haushalt", "Demo-Hausbank": "verwaltung"} {
		var start struct{ URL string }
		do("POST", "/api/connections", map[string]string{"bank": bank, "book": book}, &start)
		resp, err := c.Get(start.URL)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		want := map[string]string{"haushalt": "#konten", "verwaltung": "#hv-konten"}[book]
		if resp.Request.URL.Fragment != want[1:] {
			t.Errorf("redirect back after %s: %s", bank, resp.Request.URL)
		}
	}
	for i := 0; i < 100 && sy.Status().Running; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if err := sy.SyncAll(ctx); err != nil {
		t.Fatal(err)
	}
	var accts []store.Account
	do("GET", "/api/accounts", nil, &accts)
	var miet int64
	for _, a := range accts {
		if a.Name == "Mietkonto" {
			miet = a.ID
		}
	}
	for _, a := range accts {
		if want := map[bool]domain.Book{true: domain.BookProperty, false: domain.BookHousehold}[a.Name == "Mietkonto"]; a.Book != want {
			t.Errorf("account %s in book %s, expected %s", a.Name, a.Book, want)
		}
	}
	if code := do("PATCH", "/api/accounts/"+itoa(miet), map[string]any{"owner": "", "active": true, "book": "verwaltung"}, nil); code != 200 {
		t.Fatalf("set book: %d", code)
	}

	// Create master data
	var res struct{ ID int64 }
	do("POST", "/api/hv/properties", map[string]any{"name": "Talstraße 3", "address": "Talstraße 3, 12345 Musterstadt", "purchase_price": 45000000}, &res)
	tal := res.ID
	do("POST", "/api/hv/properties", map[string]any{"name": "ETW Beispielstadt", "address": "Rheinstraße 12, 10115 Beispielstadt"}, &res)
	etw := res.ID
	do("POST", "/api/hv/units", map[string]any{"property_id": tal, "name": "EG", "area": 7000}, &res)
	eg := res.ID
	do("POST", "/api/hv/units", map[string]any{"property_id": tal, "name": "OG", "area": 9000}, &res)
	og := res.ID
	do("POST", "/api/hv/units", map[string]any{"property_id": etw, "name": "Whg. 4", "area": 6200}, &res)
	w4 := res.ID
	start := time.Now().AddDate(-2, 0, 0).Format("2006-01-02")
	lease := func(unit int64, name, iban string, cold, nk int64, persons int) int64 {
		var r struct{ ID int64 }
		if code := do("POST", "/api/hv/leases", map[string]any{"unit_id": unit, "tenant": map[string]any{"name": name, "iban": iban},
			"start_date": start, "rent_cold": cold, "nk_prepay": nk, "due_day": 3, "persons": persons, "deposit": cold * 3, "deposit_paid": true}, &r); code != 200 {
			t.Fatalf("lease %s: %d", name, code)
		}
		return r.ID
	}
	anna := lease(eg, "Anna Schmidt", "DE21500105175555111111", 80000, 20000, 2)
	yil := lease(og, "Familie Yilmaz", "DE44700202700666222222", 95000, 30000, 4)
	weber := lease(w4, "Lukas Weber", "", 62000, 16000, 1)

	// Check assignment
	var n int
	st.DB.QueryRow(`SELECT count(*) FROM transactions WHERE lease_id IS NOT NULL AND cost_type='miete'`).Scan(&n)
	if n < 40 {
		t.Errorf("too few rents assigned: %d", n)
	}
	st.DB.QueryRow(`SELECT count(*) FROM transactions WHERE lease_id=$1`, weber).Scan(&n)
	if n < 10 {
		t.Errorf("Weber (no IBAN, by name) not assigned: %d", n)
	}
	var costTypes map[string]int
	rows, _ := st.DB.Query(`SELECT t.cost_type, count(*) FROM transactions t JOIN accounts a ON a.id=t.account_id WHERE a.book='verwaltung' GROUP BY 1`)
	costTypes = map[string]int{}
	for rows.Next() {
		var ct string
		var cnt int
		rows.Scan(&ct, &cnt)
		costTypes[ct] = cnt
	}
	rows.Close()
	for _, want := range []string{"miete", "kredit", "hausgeld", "wasser", "strom_allgemein", "entnahme", "grundsteuer", "muell", "versicherung", "schornsteinfeger", "reparatur"} {
		if costTypes[want] == 0 {
			t.Errorf("cost type %s not detected: %v", want, costTypes)
		}
	}
	if costTypes["sonstiges"] > 0 || costTypes["einnahme_sonst"] > 0 {
		t.Errorf("not assigned: %v", costTypes)
	}
	st.DB.QueryRow(`SELECT count(*) FROM transactions WHERE cost_type='hausgeld' AND property_id=$1`, etw).Scan(&n)
	if n == 0 {
		t.Errorf("Hausgeld not assigned to the ETW (condo)")
	}

	// Soll/Ist (expected vs. actual)
	var ov struct {
		Leases []struct {
			ID      int64 `json:"id"`
			Balance int64 `json:"balance"`
		} `json:"leases"`
		Totals    map[string]int64 `json:"totals"`
		Deadlines []map[string]any `json:"deadlines"`
	}
	do("GET", "/api/hv/overview", nil, &ov)
	bal := map[int64]int64{}
	for _, l := range ov.Leases {
		bal[l.ID] = l.Balance
	}
	// The current month may not be due/paid yet: check arrears relatively
	if bal[anna] != 0 && bal[anna] != 100000 {
		t.Errorf("Anna balance %d", bal[anna])
	}
	if d := bal[yil] - map[bool]int64{true: 125000, false: 0}[bal[yil] >= 125000+65000]; d != 65000 {
		t.Errorf("Yilmaz: expected partial payment 650 € outstanding, balance %d", bal[yil])
	}
	if d := bal[weber] - map[bool]int64{true: 78000, false: 0}[bal[weber] >= 2*78000]; d != 78000 {
		t.Errorf("Weber: expected one month outstanding, balance %d", bal[weber])
	}
	foundArrears := false
	for _, d := range ov.Deadlines {
		if d["kind"] == "rueckstand" {
			foundArrears = true
		}
	}
	if !foundArrears {
		t.Errorf("no arrears deadline: %v", ov.Deadlines)
	}

	// Nebenkostenabrechnung (utility cost settlement) for the previous year
	var nk struct {
		Settlement struct {
			CostTotal  int64 `json:"cost_total"`
			Statements []struct {
				Tenant  string `json:"tenant"`
				CostSum int64  `json:"cost_sum"`
				Prepaid int64  `json:"prepaid"`
			} `json:"statements"`
			OwnerShare int64 `json:"owner_share"`
		} `json:"settlement"`
	}
	year := time.Now().Year() - 1
	do("GET", "/api/hv/nk?property_id="+itoa(tal)+"&year="+itoa(int64(year)), nil, &nk)
	if nk.Settlement.CostTotal <= 0 || len(nk.Settlement.Statements) != 2 {
		t.Fatalf("NK settlement: %+v", nk.Settlement)
	}
	var sum int64
	for _, s := range nk.Settlement.Statements {
		sum += s.CostSum
		if s.Prepaid <= 0 {
			t.Errorf("no prepayments for %s", s.Tenant)
		}
	}
	if diff := nk.Settlement.CostTotal - sum - nk.Settlement.OwnerShare; diff != 0 {
		t.Errorf("sum of shares does not match: %d", diff)
	}
	// Manual costs increase the settlement
	before := nk.Settlement.CostTotal
	do("POST", "/api/hv/manual-costs", map[string]any{"property_id": tal, "year": year, "cost_type": "hausmeister", "amount": 36000}, nil)
	do("GET", "/api/hv/nk?property_id="+itoa(tal)+"&year="+itoa(int64(year)), nil, &nk)
	if nk.Settlement.CostTotal != before+36000 {
		t.Errorf("manual costs: %d -> %d", before, nk.Settlement.CostTotal)
	}

	// Property report
	var rep struct {
		Properties []struct {
			Name       string  `json:"name"`
			Income     int64   `json:"income"`
			Costs      int64   `json:"costs"`
			GrossYield float64 `json:"gross_yield"`
		} `json:"properties"`
	}
	do("GET", "/api/hv/report?year="+itoa(int64(year)), nil, &rep)
	for _, p := range rep.Properties {
		if p.Income <= 0 || p.Costs <= 0 {
			t.Errorf("property report %s: %+v", p.Name, p)
		}
		if p.Name == "Talstraße 3" && (p.GrossYield < 4.5 || p.GrossYield > 4.7) { // (800+950)×12 / 450,000 = 4.67%
			t.Errorf("gross rental yield %.2f", p.GrossYield)
		}
	}

	// Haushaltsbuch: Mietkonto (rent account) transactions do not appear; a withdrawal counts as rental income
	var txs []store.Transaction
	do("GET", "/api/transactions?limit=2000", nil, &txs)
	for _, tx := range txs {
		if tx.AccountID == miet {
			t.Fatalf("Mietkonto transaction in Haushaltsbuch: %+v", tx)
		}
	}
	st.DB.QueryRow(`SELECT count(*) FROM transactions t JOIN categories c ON c.id=t.category_id WHERE c.slug='vermietung'`).Scan(&n)
	if n == 0 {
		t.Errorf("withdrawal from Mietkonto not counted as rental income in household")
	}

	// Manually reassign a payment with "merken" (remember)
	var htx []store.HVTxn
	do("GET", "/api/hv/transactions?property_id="+itoa(tal), nil, &htx)
	var rep1 int64
	for _, tx := range htx {
		if tx.CostType == "reparatur" {
			rep1 = tx.ID
			break
		}
	}
	if code := do("PATCH", "/api/hv/transactions/"+itoa(rep1), map[string]any{"property_id": tal, "cost_type": "hausmeister", "remember": true}, nil); code != 200 {
		t.Fatalf("reassign: %d", code)
	}
	st.DB.QueryRow(`SELECT count(*) FROM transactions WHERE merchant_key=(SELECT merchant_key FROM transactions WHERE id=$1) AND cost_type<>'hausmeister'`, rep1).Scan(&n)
	if n != 0 {
		t.Errorf("rule not applied to same payee: %d", n)
	}
}
