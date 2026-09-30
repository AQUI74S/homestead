package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/AQUI74S/homestead/internal/config"
	eb "github.com/AQUI74S/homestead/internal/enablebanking"
	"github.com/AQUI74S/homestead/internal/store"
	"github.com/AQUI74S/homestead/internal/syncer"
)

// Full flow against a real Postgres with the demo bank:
// HS_TEST_DATABASE_URL=postgres://... go test ./internal/api/
func TestDemoEndToEnd(t *testing.T) {
	dsn := os.Getenv("HS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("HS_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	// fresh database
	if _, err := st.DB.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil { // idempotent
		t.Fatal(err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := eb.NewDemo()
	sy := syncer.New(st, p, log)
	cfg := config.Config{Password: "geheim", SessionKey: strings.Repeat("k", 32), Demo: true, EBCountry: "DE", EBConsentMax: 180 * 24 * time.Hour}
	web := fstest.MapFS{"index.html": {Data: []byte("<html>ok</html>")}}
	srv := httptest.NewServer(New(cfg, st, p, sy, log, web).Handler())
	defer srv.Close()
	cfg.PublicURL = srv.URL
	// Recreate the server with the correct PublicURL
	srv.Config.Handler = New(cfg, st, p, sy, log, web).Handler()

	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	do := func(method, path, body string, out any) int {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if out != nil {
			if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
				t.Fatalf("%s %s: %v", method, path, err)
			}
		}
		return resp.StatusCode
	}

	if code := do("GET", "/api/overview", "", nil); code != 401 {
		t.Fatalf("without login: %d", code)
	}
	if code := do("POST", "/api/login", `{"password":"falsch"}`, nil); code != 401 {
		t.Fatalf("wrong password: %d", code)
	}
	if code := do("POST", "/api/login", `{"password":"geheim"}`, nil); code != 200 {
		t.Fatalf("login: %d", code)
	}

	// Empty database: overview must return lists instead of null
	var empty map[string]any
	do("GET", "/api/overview", "", &empty)
	for _, k := range []string{"accounts", "trend"} {
		if _, ok := empty[k].([]any); !ok {
			t.Errorf("empty overview: %s is %v instead of a list", k, empty[k])
		}
	}
	if fc, ok := empty["forecast"].(map[string]any); !ok {
		t.Errorf("empty overview without forecast")
	} else if _, ok := fc["items"].([]any); !ok {
		t.Errorf("forecast items are %v instead of a list", fc["items"])
	}
	for _, path := range []string{"/api/accounts", "/api/connections", "/api/rules", "/api/recurring", "/api/transactions"} {
		var v any
		do("GET", path, "", &v)
		if _, ok := v.([]any); !ok {
			t.Errorf("%s returns %v instead of a list", path, v)
		}
	}

	// Connect both demo banks (the client follows the redirect chain automatically)
	for _, bank := range []string{"Demo-Sparkasse", "Demo-Direktbank"} {
		var start struct{ URL string }
		if code := do("POST", "/api/connections", `{"bank":"`+bank+`"}`, &start); code != 200 || start.URL == "" {
			t.Fatalf("start connection %s: %d", bank, code)
		}
		resp, err := c.Get(start.URL)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if !strings.Contains(resp.Request.URL.RawQuery, "bank=ok") {
			t.Fatalf("callback %s ended at %s", bank, resp.Request.URL)
		}
	}
	// Sync runs in the background; run it again synchronously and wait
	for i := 0; i < 100 && sy.Status().Running; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if err := sy.SyncAll(ctx); err != nil {
		t.Fatal(err)
	}

	var accts []store.Account
	do("GET", "/api/accounts", "", &accts)
	if len(accts) != 3 {
		t.Fatalf("accounts: %d", len(accts))
	}
	for _, a := range accts {
		owner := map[string]string{"DE12500105170648489890": "A", "DE75512108001245126199": "B"}[a.IBAN]
		if code := do("PATCH", "/api/accounts/"+itoa(a.ID), `{"owner":"`+owner+`","active":true}`, nil); code != 200 {
			t.Fatalf("update account: %d", code)
		}
		if a.BalanceCents == nil {
			t.Errorf("account %s without balance", a.Name)
		}
	}

	// Check classification: share of uncategorized expenses
	var stats struct{ Total, Sonst, Transfer int }
	st.DB.QueryRow(`SELECT count(*), count(*) FILTER (WHERE c.slug IN ('sonstiges','einnahmen-sonst')), count(*) FILTER (WHERE c.slug='umbuchung')
		FROM transactions t JOIN categories c ON c.id=t.category_id`).Scan(&stats.Total, &stats.Sonst, &stats.Transfer)
	if stats.Total < 500 {
		t.Fatalf("too few transactions: %d", stats.Total)
	}
	if float64(stats.Sonst) > 0.05*float64(stats.Total) {
		t.Errorf("too many uncategorized transactions: %d of %d", stats.Sonst, stats.Total)
	}
	if stats.Transfer == 0 {
		t.Errorf("no transfers detected")
	}

	var rec []store.Recurring
	do("GET", "/api/recurring", "", &rec)
	kinds := map[string]string{}
	for _, r := range rec {
		kinds[r.Label] = r.Kind + "/" + r.CycleLabel
		if r.Ended {
			kinds[r.Label] += "/beendet"
		}
	}
	want := map[string]string{
		"Netflix International B.V.": "abo/Monatlich",
		"Spotify AB":                 "abo/Monatlich",
		"Hetzner Online GmbH":        "abo/Monatlich",
		"Disney Plus":                "abo/Monatlich",
		"Audible GmbH":               "abo/Monatlich/beendet",
		"Muster Software GmbH":       "einkommen/Monatlich",
		"Rundfunk ARD, ZDF, DRadio":  "fixkosten/Vierteljährlich",
		"Santander Consumer Bank AG": "kredit/Monatlich",
		"Trade Republic Bank GmbH":   "sparen/Monatlich",
		"ADAC e.V.":                  "abo/Jährlich",
	}
	for label, k := range want {
		if kinds[label] != k {
			t.Errorf("series %q: %q, expected %q", label, kinds[label], k)
		}
	}

	// Overview of the current month
	var ov struct {
		Report     store.MonthReport
		AboMonthly int64 `json:"abo_monthly"`
	}
	prev := time.Now().AddDate(0, -1, 0).Format("2006-01")
	do("GET", "/api/overview?month="+prev, "", &ov)
	var income int64
	for _, l := range ov.Report.Lines {
		if l.Group == "income" {
			income += l.IstCents
		}
	}
	if income <= 0 || ov.Report.IncomeBy["A"] <= 0 || ov.Report.IncomeBy["B"] <= 0 {
		t.Errorf("income missing: %d, by person %v", income, ov.Report.IncomeBy)
	}
	if ov.AboMonthly < 10000 {
		t.Errorf("subscription total implausible: %d", ov.AboMonthly)
	}

	// Recategorize with a rule: all Lieferando transactions to "Freizeit"
	var txs []store.Transaction
	do("GET", "/api/transactions?q=Lieferando&limit=5", "", &txs)
	if len(txs) == 0 {
		t.Fatal("no Lieferando transactions")
	}
	var cats []store.Category
	do("GET", "/api/categories", "", &cats)
	var freizeit int64
	for _, c := range cats {
		if c.Slug == "freizeit" {
			freizeit = c.ID
		}
	}
	if code := do("PATCH", "/api/transactions/"+itoa(txs[0].ID), `{"category_id":`+itoa(freizeit)+`,"rule":true}`, nil); code != 200 {
		t.Fatalf("recategorize: %d", code)
	}
	var n int
	st.DB.QueryRow(`SELECT count(*) FROM transactions t JOIN categories c ON c.id=t.category_id WHERE t.merchant_key='lieferando' AND c.slug<>'freizeit'`).Scan(&n)
	if n != 0 {
		t.Errorf("rule not applied: %d Lieferando transactions elsewhere", n)
	}

	// Suggest budgets
	var sug struct{ Updated int }
	do("POST", "/api/budgets/suggest", "", &sug)
	if sug.Updated == 0 {
		t.Errorf("no budgets suggested")
	}

	// Budget month from payday to payday
	var me struct {
		CurrentMonth string `json:"current_month"`
		SalaryName   string `json:"salary_series_name"`
	}
	do("GET", "/api/me", "", &me)
	if me.SalaryName != "Muster Software GmbH" {
		t.Errorf("salary series: %q", me.SalaryName)
	}
	var ov2 struct {
		Period struct {
			Month, Start, End, Mode string
		}
		Forecast struct {
			Items     []map[string]any `json:"items"`
			OpenOut   int64            `json:"open_out"`
			Projected int64            `json:"projected"`
			Past      bool             `json:"past"`
		}
	}
	prevMonth := time.Now().AddDate(0, -1, 0).Format("2006-01")
	do("GET", "/api/overview?month="+prevMonth, "", &ov2)
	if ov2.Period.Mode != "salary" {
		t.Fatalf("period: %+v", ov2.Period)
	}
	var salaryDates []string
	st.DB.QueryRow(`SELECT string_agg(to_char(t.booking_date,'YYYY-MM-DD'), ',') FROM transactions t JOIN recurring r ON r.id=t.recurring_id WHERE r.label='Muster Software GmbH'`).Scan(new(string))
	rows, _ := st.DB.Query(`SELECT to_char(t.booking_date,'YYYY-MM-DD') FROM transactions t JOIN recurring r ON r.id=t.recurring_id WHERE r.label='Muster Software GmbH' ORDER BY 1`)
	for rows.Next() {
		var d string
		rows.Scan(&d)
		salaryDates = append(salaryDates, d)
	}
	rows.Close()
	found := false
	for _, d := range salaryDates {
		if d == ov2.Period.Start {
			found = true
		}
	}
	if !found {
		t.Errorf("period start %s is not a salary date (%v)", ov2.Period.Start, salaryDates)
	}
	// Period must be named after the month the salary is meant for
	st0, _ := time.Parse("2006-01-02", ov2.Period.Start)
	if st0.AddDate(0, 0, 10).Format("2006-01") != prevMonth {
		t.Errorf("period %s starts %s", prevMonth, ov2.Period.Start)
	}

	// "Demnächst abgebucht" (upcoming debits): 30 days, each payment with an account
	var up struct {
		Upcoming []struct {
			Label     string `json:"label"`
			AccountID *int64 `json:"account_id"`
			Amount    int64  `json:"amount"`
		} `json:"upcoming"`
	}
	do("GET", "/api/overview", "", &up)
	if len(up.Upcoming) < 10 {
		t.Errorf("upcoming debits: only %d entries", len(up.Upcoming))
	}
	for _, u := range up.Upcoming {
		if u.AccountID == nil {
			t.Errorf("%s without account", u.Label)
		}
	}

	// Future month: forecast includes salary and fixed costs
	next := time.Now().AddDate(0, 2, 0).Format("2006-01")
	do("GET", "/api/overview?month="+next, "", &ov2)
	if ov2.Forecast.Past || ov2.Forecast.OpenOut <= 0 || len(ov2.Forecast.Items) < 10 {
		t.Errorf("future forecast: past=%v open_out=%d items=%d", ov2.Forecast.Past, ov2.Forecast.OpenOut, len(ov2.Forecast.Items))
	}

	// Create a contract manually: shows up in the forecast of its due month
	nextStart := ov2.Period.Start
	due, _ := time.Parse("2006-01-02", nextStart)
	due = due.AddDate(0, 0, 5)
	var created struct{ ID int64 }
	if code := do("POST", "/api/recurring", `{"label":"Kfz-Steuer","amount":14200,"cycle_days":365,"next_date":"`+due.Format("2006-01-02")+`","kind":"fixkosten"}`, &created); code != 201 {
		t.Fatalf("create contract: %d", code)
	}
	before := ov2.Forecast.OpenOut
	do("GET", "/api/overview?month="+next, "", &ov2)
	if ov2.Forecast.OpenOut != before+14200 {
		t.Errorf("contract not in forecast: %d -> %d", before, ov2.Forecast.OpenOut)
	}
	if err := sy.SyncAll(ctx); err != nil { // sync must not delete manual contracts
		t.Fatal(err)
	}
	var cnt int
	st.DB.QueryRow(`SELECT count(*) FROM recurring WHERE manual`).Scan(&cnt)
	if cnt != 1 {
		t.Errorf("manual contract after sync: %d", cnt)
	}
	// Contract with a past date (last payment): next due date is computed
	past := time.Now().AddDate(0, -3, 0).Format("2006-01-02")
	var created2 struct{ ID int64 }
	do("POST", "/api/recurring", `{"label":"Kfz halbjährlich","amount":59026,"cycle_days":182,"next_date":"`+past+`","kind":"fixkosten"}`, &created2)
	var nextDate string
	st.DB.QueryRow(`SELECT to_char(next_date,'YYYY-MM-DD') FROM recurring WHERE id=$1`, created2.ID).Scan(&nextDate)
	if want := time.Now().AddDate(0, 3, 0).Format("2006-01-02"); nextDate != want {
		t.Errorf("next due date from last payment: %s, expected %s", nextDate, want)
	}
	do("DELETE", "/api/recurring/"+itoa(created2.ID), "", nil)
	if code := do("DELETE", "/api/recurring/"+itoa(created.ID), "", nil); code != 200 {
		t.Errorf("delete contract: %d", code)
	}

	// CSV import of older transactions with duplicate protection
	var giro int64
	for _, a := range accts {
		if a.IBAN == "DE12500105170648489890" {
			giro = a.ID
		}
	}
	var dupDate string
	var dupAmount int64
	st.DB.QueryRow(`SELECT to_char(booking_date,'DD.MM.YYYY'), (amount*100)::bigint FROM transactions WHERE account_id=$1 ORDER BY booking_date LIMIT 1`, giro).Scan(&dupDate, &dupAmount)
	csvBody := "Buchungstag;Beguenstigter/Zahlungspflichtiger;Verwendungszweck;Kontonummer/IBAN;Betrag\n" +
		"15.01.2023;HUK-COBURG;Kfz-Versicherung 2023;DE36;-598,00\n" +
		"15.01.2024;HUK-COBURG;Kfz-Versicherung 2024;DE36;-605,00\n" +
		dupDate + ";X;schon da;;" + strings.Replace(fmt.Sprintf("%.2f", float64(dupAmount)/100), ".", ",", 1) + "\n"
	var imp struct{ Added, Skipped int }
	code := upload(t, c, srv.URL+"/api/accounts/"+itoa(giro)+"/import", csvBody, &imp)
	if code != 200 || imp.Added != 2 || imp.Skipped != 1 {
		t.Errorf("CSV import: code=%d %+v", code, imp)
	}

	// A second sync must not create duplicates
	st.DB.QueryRow(`SELECT count(*) FROM transactions`).Scan(&stats.Total)
	totalBefore := stats.Total
	if err := sy.SyncAll(ctx); err != nil {
		t.Fatal(err)
	}
	st.DB.QueryRow(`SELECT count(*) FROM transactions`).Scan(&stats.Total)
	if stats.Total != totalBefore {
		t.Errorf("duplicates after second sync: %d -> %d", totalBefore, stats.Total)
	}
}

func itoa(i int64) string {
	b, _ := json.Marshal(i)
	return string(b)
}

func upload(t *testing.T, c *http.Client, url, body string, out any) int {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "umsaetze.csv")
	fw.Write([]byte(body))
	mw.Close()
	req, _ := http.NewRequest("POST", url, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	json.NewDecoder(resp.Body).Decode(out)
	return resp.StatusCode
}
