// Package api provides the REST API and the web UI.
package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/AQUI74S/homestead/internal/classify"
	"github.com/AQUI74S/homestead/internal/config"
	"github.com/AQUI74S/homestead/internal/csvimport"
	eb "github.com/AQUI74S/homestead/internal/enablebanking"
	"github.com/AQUI74S/homestead/internal/forecast"
	"github.com/AQUI74S/homestead/internal/store"
	"github.com/AQUI74S/homestead/internal/syncer"
)

type Server struct {
	cfg  config.Config
	st   *store.Store
	p    eb.Provider
	sync *syncer.Syncer
	log  *slog.Logger
	web  fs.FS

	banksMu    chan struct{}
	banksCache []eb.ASPSP
	banksAt    time.Time
}

func New(cfg config.Config, st *store.Store, p eb.Provider, sy *syncer.Syncer, log *slog.Logger, web fs.FS) *Server {
	return &Server{cfg: cfg, st: st, p: p, sync: sy, log: log, web: web, banksMu: make(chan struct{}, 1)}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/me", s.me)
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.logout)

	a := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, s.auth(h)) }
	a("GET /api/overview", s.overview)
	a("GET /api/categories", s.categories)
	a("POST /api/categories", s.createCategory)
	a("PUT /api/categories/{id}/budget", s.setBudget)
	a("POST /api/budgets/suggest", s.suggestBudgets)
	a("GET /api/transactions", s.transactions)
	a("PATCH /api/transactions/{id}", s.patchTransaction)
	a("GET /api/recurring", s.recurring)
	a("PATCH /api/recurring/{id}", s.patchRecurring)
	a("POST /api/recurring", s.createRecurring)
	a("DELETE /api/recurring/{id}", s.deleteRecurring)
	a("POST /api/accounts/{id}/import", s.importCSV)
	a("GET /api/rules", s.rules)
	a("DELETE /api/rules/{id}", s.deleteRule)
	a("GET /api/banks", s.banks)
	a("GET /api/accounts", s.accounts)
	a("PATCH /api/accounts/{id}", s.patchAccount)
	a("GET /api/connections", s.connections)
	a("POST /api/connections", s.startConnection)
	a("GET /api/connections/callback", s.callback)
	a("DELETE /api/connections/{id}", s.deleteConnection)
	a("POST /api/sync", s.triggerSync)
	a("GET /api/sync", s.syncStatus)
	a("PUT /api/settings", s.putSettings)
	s.hvRoutes(a)

	mux.Handle("GET /", s.static())
	return s.securityHeaders(mux)
}

// ---------- Helpers ----------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, 404, map[string]string{"error": "Nicht gefunden"})
	default:
		s.log.Error("API error", "path", r.URL.Path, "err", err)
		writeJSON(w, 500, map[string]string{"error": err.Error()})
	}
}

func bad(w http.ResponseWriter, msg string) { writeJSON(w, 400, map[string]string{"error": msg}) }

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	return dec.Decode(v)
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func (s *Server) securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		h.ServeHTTP(w, r)
	})
}

func (s *Server) static() http.Handler {
	fsrv := http.FileServer(http.FS(s.web))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeJSON(w, 404, map[string]string{"error": "Unbekannter Endpunkt"})
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		fsrv.ServeHTTP(w, r)
	})
}

// ---------- Login ----------

const cookieName = "homestead_session"

func (s *Server) sign(exp int64) string {
	m := hmac.New(sha256.New, []byte(s.cfg.SessionKey))
	fmt.Fprintf(m, "%d", exp)
	return fmt.Sprintf("%d.%s", exp, hex.EncodeToString(m.Sum(nil)))
}

func (s *Server) validCookie(r *http.Request) bool {
	if s.cfg.Password == "" {
		return true
	}
	c, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	expStr, _, ok := strings.Cut(c.Value, ".")
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if !ok || err != nil || time.Now().Unix() > exp {
		return false
	}
	return hmac.Equal([]byte(c.Value), []byte(s.sign(exp)))
}

func (s *Server) auth(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.validCookie(r) {
			if r.URL.Path == "/api/connections/callback" { // browser returning from the bank
				http.Redirect(w, r, "/#login", http.StatusFound)
				return
			}
			writeJSON(w, 401, map[string]string{"error": "Bitte anmelden"})
			return
		}
		h(w, r)
	})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	settings, _ := s.st.Settings(r.Context())
	out := map[string]any{
		"logged_in":     s.validCookie(r),
		"auth_required": s.cfg.Password != "",
		"demo":          s.cfg.Demo,
		"name_a":        settings["name_a"],
		"name_b":        settings["name_b"],
		"current_month": time.Now().Format("2006-01"),
	}
	if s.validCookie(r) {
		if pc, err := s.st.PeriodCalc(r.Context()); err == nil {
			out["current_month"] = pc.Current(time.Now())
			out["period_mode"] = settings["period_mode"]
			out["salary_series"] = settings["salary_series"]
			out["salary_series_name"] = pc.SeriesName
			out["own_names"] = settings["own_names"]
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct{ Password string }
	if err := decode(r, &in); err != nil {
		bad(w, "Ungültige Anfrage")
		return
	}
	if s.cfg.Password == "" || subtle.ConstantTimeCompare([]byte(in.Password), []byte(s.cfg.Password)) != 1 {
		time.Sleep(700 * time.Millisecond) // slows down guessing attempts
		writeJSON(w, 401, map[string]string{"error": "Passwort stimmt nicht"})
		return
	}
	exp := time.Now().Add(30 * 24 * time.Hour).Unix()
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: s.sign(exp), Path: "/", HttpOnly: true,
		Secure: strings.HasPrefix(s.cfg.PublicURL, "https://"), SameSite: http.SameSiteLaxMode, Expires: time.Unix(exp, 0)})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---------- Overview & budgets ----------

func month(r *http.Request, pc *store.PeriodCalc) string {
	m := r.URL.Query().Get("month")
	if _, err := time.Parse("2006-01", m); err != nil {
		return pc.Current(time.Now())
	}
	return m
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pc, err := s.st.PeriodCalc(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	m := month(r, pc)
	rep, err := s.st.Report(ctx, pc, m)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if avg, n, err := s.st.BudgetAverages(ctx, pc, time.Now()); err == nil {
		rep.AvgPeriods = n
		for i := range rep.Lines {
			rep.Lines[i].AvgCents = avg[rep.Lines[i].ID]
		}
	}
	trend, err := s.st.Trend(ctx, pc, m, 12)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	rec, err := s.st.RecurringList(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	accts, rec, err := s.householdOnly(ctx, rec)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var aboMonthly, fixMonthly int64
	recCats := map[int64]bool{}
	for i := range rec {
		x := &rec[i]
		enrich(x)
		if x.Ended || x.Status == "ignored" || x.Direction != "out" {
			continue
		}
		if x.CategoryID != nil {
			recCats[*x.CategoryID] = true
		}
		switch x.Kind {
		case "abo":
			aboMonthly += x.MonthlyCents
		case "fixkosten", "kredit":
			fixMonthly += x.MonthlyCents
		}
	}

	// Forecast until end of period
	start, end, _ := pc.Range(m)
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	fc := forecast.Compute(rec, start, end, today)
	if pc.Mode == "salary" {
		fc.DropNextSalary(pc.SeriesID, start)
	}
	var incomeIst, outIst, budgetRest int64
	for _, l := range rep.Lines {
		switch l.Group {
		case "income":
			incomeIst += l.IstCents
		case "transfer":
		default:
			outIst += l.IstCents
			// Remaining budget for variable spending, unless already expected as a recurring payment
			if l.Group == "expenses" && !recCats[l.ID] && l.BudgetCents > l.IstCents {
				budgetRest += l.BudgetCents - l.IstCents
			}
		}
	}
	// "Demnächst abgebucht" (upcoming debits): next 30 days, independent of the budget month
	up := forecast.Compute(rec, today, today.AddDate(0, 0, 31), today)
	upcoming := []forecast.Item{}
	for _, it := range up.Items {
		if it.Status != "bezahlt" {
			upcoming = append(upcoming, it)
		}
	}
	past := !end.After(today)
	if past {
		fc = forecast.Result{Items: fc.Items, OpenByKind: map[string]int64{}}
		budgetRest = 0
	}
	settings, _ := s.st.Settings(ctx)
	writeJSON(w, 200, map[string]any{
		"report": rep, "trend": trend, "accounts": accts, "sync": s.sync.Status(),
		"abo_monthly": aboMonthly, "fixed_monthly": fixMonthly,
		"period": pc.Describe(m), "current_month": pc.Current(now), "upcoming": upcoming,
		"forecast": map[string]any{
			"items": fc.Items, "open_in": fc.OpenIn, "open_out": fc.OpenOut, "open_by_kind": fc.OpenByKind,
			"budget_rest": budgetRest, "income_ist": incomeIst, "out_ist": outIst, "past": past,
			"projected": incomeIst + fc.OpenIn - outIst - fc.OpenOut - budgetRest,
		},
		"name_a": settings["name_a"], "name_b": settings["name_b"],
	})
}

func (s *Server) categories(w http.ResponseWriter, r *http.Request) {
	cats, err := s.st.Categories(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, cats)
}

var validGroups = map[string]bool{"income": true, "bills": true, "expenses": true, "savings": true, "debts": true, "transfer": true}

func (s *Server) createCategory(w http.ResponseWriter, r *http.Request) {
	var in struct{ Group, Name string }
	if err := decode(r, &in); err != nil || !validGroups[in.Group] || strings.TrimSpace(in.Name) == "" {
		bad(w, "Bereich und Name angeben")
		return
	}
	id, err := s.st.CreateCategory(r.Context(), in.Group, strings.TrimSpace(in.Name))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]int64{"id": id})
}

func (s *Server) setBudget(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	var in struct {
		Amount int64  `json:"amount"` // cents
		Month  string `json:"month"`  // empty = default budget
	}
	if !ok || decode(r, &in) != nil || in.Amount < 0 {
		bad(w, "Betrag in Cent angeben")
		return
	}
	if in.Month != "" {
		if _, err := time.Parse("2006-01", in.Month); err != nil {
			bad(w, "Monat im Format JJJJ-MM angeben")
			return
		}
	}
	if err := s.st.SetBudget(r.Context(), id, in.Month, in.Amount); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) suggestBudgets(w http.ResponseWriter, r *http.Request) {
	pc, err := s.st.PeriodCalc(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in struct {
		Overwrite bool `json:"overwrite"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in) // empty body = only missing budgets
	n, months, err := s.st.SuggestBudgets(r.Context(), pc, time.Now(), in.Overwrite)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]int{"updated": n, "months": months})
}

// ---------- Transactions ----------

func (s *Server) transactions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	num := func(k string) int64 { v, _ := strconv.ParseInt(q.Get(k), 10, 64); return v }
	book := q.Get("book")
	switch book {
	case "":
		book = "haushalt"
	case "alle":
		book = ""
	}
	f := store.TxnFilter{Month: q.Get("month"), CategoryID: num("category_id"), Group: q.Get("group"),
		AccountID: num("account_id"), Search: q.Get("q"), RecurringID: num("recurring_id"), Limit: int(num("limit")),
		Book: book, LeaseID: num("lease_id"), PropertyID: num("property_id")}
	txs, err := s.st.Transactions(r.Context(), f)
	if err != nil {
		if strings.HasPrefix(err.Error(), "Monat") {
			bad(w, err.Error())
			return
		}
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, txs)
}

func (s *Server) patchTransaction(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	var in struct {
		CategoryID int64   `json:"category_id"`
		Note       *string `json:"note"`
		Rule       bool    `json:"rule"`  // create a rule for this merchant
		Reset      bool    `json:"reset"` // revert to automatic categorization
	}
	if !ok || decode(r, &in) != nil {
		bad(w, "Ungültige Anfrage")
		return
	}
	ctx := r.Context()
	if in.Reset {
		if err := s.st.ResetTransactionCategory(ctx, id); err != nil {
			s.fail(w, r, err)
			return
		}
	} else {
		if in.CategoryID <= 0 {
			bad(w, "Kategorie wählen")
			return
		}
		if err := s.st.SetTransactionCategory(ctx, id, in.CategoryID, in.Note); err != nil {
			s.fail(w, r, err)
			return
		}
		if in.Rule {
			t, err := s.st.TransactionByID(ctx, id)
			if err != nil {
				s.fail(w, r, err)
				return
			}
			field, pattern := "merchant", t.MerchantKey
			if pattern == "" && t.CounterpartyIBAN != "" {
				field, pattern = "iban", t.CounterpartyIBAN
			}
			if pattern == "" {
				bad(w, "Für diesen Umsatz lässt sich keine Regel ableiten (kein Händlername)")
				return
			}
			if err := s.st.UpsertRule(ctx, field, pattern, in.CategoryID); err != nil {
				s.fail(w, r, err)
				return
			}
		}
	}
	if err := s.sync.Reclassify(ctx); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---------- Recurring payments & rules ----------

func enrich(x *store.Recurring) {
	x.CycleLabel = classify.CycleLabel(x.CycleDays)
	x.MonthlyCents = classify.Series{MedianCents: x.AvgCents, CycleDays: x.CycleDays}.MonthlyCents()
}

// householdOnly filters accounts and series down to the Haushaltsbuch (household book).
func (s *Server) householdOnly(ctx context.Context, rec []store.Recurring) ([]store.Account, []store.Recurring, error) {
	accts, err := s.st.Accounts(ctx)
	if err != nil {
		return nil, nil, err
	}
	book := map[int64]string{}
	hh := []store.Account{}
	for _, a := range accts {
		book[a.ID] = a.Book
		if a.Book != "verwaltung" {
			hh = append(hh, a)
		}
	}
	out := []store.Recurring{}
	for _, x := range rec {
		if x.AccountID == nil || book[*x.AccountID] != "verwaltung" {
			out = append(out, x)
		}
	}
	return hh, out, nil
}

func (s *Server) recurring(w http.ResponseWriter, r *http.Request) {
	rec, err := s.st.RecurringList(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if _, rec, err = s.householdOnly(r.Context(), rec); err != nil {
		s.fail(w, r, err)
		return
	}
	for i := range rec {
		enrich(&rec[i])
	}
	writeJSON(w, 200, rec)
}

var validKinds = map[string]bool{"": true, "abo": true, "fixkosten": true, "einkommen": true, "kredit": true, "sparen": true, "sonstiges": true}
var validRecStatus = map[string]bool{"": true, "detected": true, "confirmed": true, "ignored": true}

func (s *Server) patchRecurring(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	var in struct{ Status, Kind string }
	if !ok || decode(r, &in) != nil || !validKinds[in.Kind] || !validRecStatus[in.Status] {
		bad(w, "Ungültige Art oder Status")
		return
	}
	if err := s.st.UpdateRecurring(r.Context(), id, in.Status, in.Kind); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) rules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.st.Rules(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, rules)
}

func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		bad(w, "Ungültige ID")
		return
	}
	if err := s.st.DeleteRule(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.sync.Reclassify(r.Context()); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---------- Banks, accounts, connections ----------

func (s *Server) listBanks(ctx context.Context) ([]eb.ASPSP, error) {
	s.banksMu <- struct{}{}
	defer func() { <-s.banksMu }()
	if s.banksCache != nil && time.Since(s.banksAt) < 12*time.Hour {
		return s.banksCache, nil
	}
	banks, err := s.p.ASPSPs(ctx, s.cfg.EBCountry)
	if err != nil {
		return nil, err
	}
	sort.Slice(banks, func(i, j int) bool { return strings.ToLower(banks[i].Name) < strings.ToLower(banks[j].Name) })
	s.banksCache, s.banksAt = banks, time.Now()
	return banks, nil
}

func (s *Server) banks(w http.ResponseWriter, r *http.Request) {
	banks, err := s.listBanks(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	type bank struct {
		Name    string `json:"name"`
		Country string `json:"country"`
		Logo    string `json:"logo"`
		Beta    bool   `json:"beta"`
		Days    int64  `json:"consent_days"`
	}
	out := []bank{}
	for _, b := range banks {
		out = append(out, bank{b.Name, b.Country, b.Logo, b.Beta, b.MaximumConsentValidity / 86400})
	}
	writeJSON(w, 200, out)
}

func (s *Server) accounts(w http.ResponseWriter, r *http.Request) {
	a, err := s.st.Accounts(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if a == nil {
		a = []store.Account{}
	}
	writeJSON(w, 200, a)
}

func (s *Server) patchAccount(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	var in struct {
		Owner       string `json:"owner"`
		DisplayName string `json:"display_name"`
		Active      *bool  `json:"active"`
		Book        string `json:"book"`
	}
	if !ok || decode(r, &in) != nil || (in.Owner != "A" && in.Owner != "B" && in.Owner != "") {
		bad(w, "Inhaber muss A, B oder leer (gemeinsam) sein")
		return
	}
	active := true
	if in.Active != nil {
		active = *in.Active
	}
	if err := s.st.UpdateAccount(r.Context(), id, in.Owner, strings.TrimSpace(in.DisplayName), active, in.Book); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.sync.Reclassify(r.Context()); err != nil { // re-evaluate transfers between the books
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) connections(w http.ResponseWriter, r *http.Request) {
	c, err := s.st.Connections(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if c == nil {
		c = []store.Connection{}
	}
	writeJSON(w, 200, c)
}

func randomState() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) startConnection(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Bank    string `json:"bank"`
		Country string `json:"country"`
		Book    string `json:"book"`
	}
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Bank) == "" {
		bad(w, "Bank auswählen")
		return
	}
	if in.Country == "" {
		in.Country = s.cfg.EBCountry
	}
	ctx := r.Context()
	valid := s.cfg.EBConsentMax
	if banks, err := s.listBanks(ctx); err == nil {
		for _, b := range banks {
			if b.Name == in.Bank && b.Country == in.Country && b.MaximumConsentValidity > 0 {
				if d := time.Duration(b.MaximumConsentValidity) * time.Second; d < valid {
					valid = d
				}
			}
		}
	}
	state := randomState()
	if _, err := s.st.CreatePendingConnection(ctx, in.Bank, in.Country, state, in.Book); err != nil {
		s.fail(w, r, err)
		return
	}
	u, err := s.p.StartAuth(ctx, eb.AuthRequest{ASPSPName: in.Bank, ASPSPCountry: in.Country, State: state,
		RedirectURL: s.cfg.PublicURL + "/api/connections/callback", ValidUntil: time.Now().Add(valid - time.Hour)})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]string{"url": u})
}

func (s *Server) callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	target := "#konten"
	if c, err := s.st.ConnectionByState(r.Context(), q.Get("state")); err == nil && c.Book == "verwaltung" {
		target = "#hv-konten"
	}
	back := func(msg string) {
		http.Redirect(w, r, "/?"+url.Values{"bank": {msg}}.Encode()+target, http.StatusFound)
	}
	if e := q.Get("error"); e != "" {
		if c, err := s.st.ConnectionByState(r.Context(), q.Get("state")); err == nil {
			_ = s.st.SetConnectionStatus(r.Context(), c.ID, "error", e+": "+q.Get("error_description"))
		}
		back("abgebrochen")
		return
	}
	if q.Get("code") == "" || q.Get("state") == "" {
		back("fehler")
		return
	}
	if _, err := s.sync.CompleteAuth(r.Context(), q.Get("state"), q.Get("code")); err != nil {
		s.log.Error("bank authorization failed", "err", err)
		back("fehler")
		return
	}
	back("ok")
}

func (s *Server) deleteConnection(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		bad(w, "Ungültige ID")
		return
	}
	c, err := s.st.Connection(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if c.SessionID != "" {
		if err := s.p.DeleteSession(r.Context(), c.SessionID); err != nil && !eb.SessionInvalid(err) {
			s.log.Warn("session not deleted at Enable Banking", "err", err)
		}
	}
	if err := s.st.SetConnectionStatus(r.Context(), id, "revoked", "Vom Nutzer getrennt"); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) triggerSync(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 202, map[string]bool{"started": s.sync.TriggerAsync()})
}

func (s *Server) syncStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.sync.Status())
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		NameA        string  `json:"name_a"`
		NameB        string  `json:"name_b"`
		OwnNames     *string `json:"own_names"`     // additional own names, comma-separated
		PeriodMode   string  `json:"period_mode"`   // salary | calendar
		SalarySeries *string `json:"salary_series"` // series ID or "" for automatic
	}
	if err := decode(r, &in); err != nil {
		bad(w, "Ungültige Anfrage")
		return
	}
	if in.PeriodMode != "" {
		if in.PeriodMode != "salary" && in.PeriodMode != "calendar" {
			bad(w, "Budgetmonat: salary oder calendar")
			return
		}
		if err := s.st.SetSetting(r.Context(), "period_mode", in.PeriodMode); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	if in.SalarySeries != nil {
		if *in.SalarySeries != "" {
			if _, err := strconv.ParseInt(*in.SalarySeries, 10, 64); err != nil {
				bad(w, "Ungültige Gehaltsserie")
				return
			}
		}
		if err := s.st.SetSetting(r.Context(), "salary_series", *in.SalarySeries); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	if in.OwnNames != nil {
		if err := s.st.SetSetting(r.Context(), "own_names", strings.TrimSpace(*in.OwnNames)); err != nil {
			s.fail(w, r, err)
			return
		}
		if err := s.sync.Reclassify(r.Context()); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	for k, v := range map[string]string{"name_a": in.NameA, "name_b": in.NameB} {
		if v = strings.TrimSpace(v); v != "" {
			if err := s.st.SetSetting(r.Context(), k, v); err != nil {
				s.fail(w, r, err)
				return
			}
		}
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

var validCycles = map[int]bool{7: true, 14: true, 30: true, 61: true, 91: true, 182: true, 365: true}

func (s *Server) createRecurring(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Label      string `json:"label"`
		Amount     int64  `json:"amount"` // cents, positive
		CycleDays  int    `json:"cycle_days"`
		NextDate   string `json:"next_date"`
		Kind       string `json:"kind"`
		CategoryID int64  `json:"category_id"`
		AccountID  int64  `json:"account_id"`
	}
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Label) == "" || in.Amount <= 0 {
		bad(w, "Name und Betrag angeben")
		return
	}
	if !validCycles[in.CycleDays] {
		bad(w, "Rhythmus wählen")
		return
	}
	next, err := time.Parse("2006-01-02", in.NextDate)
	if err != nil {
		bad(w, "Nächste Fälligkeit als Datum angeben")
		return
	}
	if in.Kind == "" || !validKinds[in.Kind] {
		in.Kind = "fixkosten"
	}
	dir := "out"
	if in.Kind == "einkommen" {
		dir = "in"
	}
	var cat *int64
	if in.CategoryID > 0 {
		cat = &in.CategoryID
	}
	var acc *int64
	if in.AccountID > 0 {
		acc = &in.AccountID
	}
	id, err := s.st.CreateManualRecurring(r.Context(), store.ManualRecurring{Label: strings.TrimSpace(in.Label), Direction: dir,
		Kind: in.Kind, CycleDays: in.CycleDays, AmountCents: in.Amount, NextDate: next, CategoryID: cat, AccountID: acc})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]int64{"id": id})
}

func (s *Server) deleteRecurring(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		bad(w, "Ungültige ID")
		return
	}
	if err := s.st.DeleteManualRecurring(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			bad(w, "Nur von Hand angelegte Verträge lassen sich löschen. Erkannte Zahlungen bitte ignorieren.")
			return
		}
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) importCSV(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		bad(w, "Ungültiges Konto")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 20<<20)
	f, _, err := r.FormFile("file")
	if err != nil {
		bad(w, "CSV-Datei fehlt")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		bad(w, "Datei nicht lesbar")
		return
	}
	rows, err := csvimport.Parse(data)
	if err != nil {
		bad(w, err.Error())
		return
	}
	seen := map[string]int{}
	txs := make([]store.NewTxn, 0, len(rows))
	for _, row := range rows {
		h := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s|%s", row.BookingDate.Format("2006-01-02"), row.AmountCents, row.Counterparty, row.Remittance)))
		base := "csv:" + hex.EncodeToString(h[:12])
		seen[base]++
		txs = append(txs, store.NewTxn{ExtID: fmt.Sprintf("%s:%d", base, seen[base]), BookingDate: row.BookingDate,
			AmountCents: row.AmountCents, Currency: "EUR", Counterparty: row.Counterparty, CounterpartyIBAN: row.IBAN, Remittance: row.Remittance})
	}
	added, skipped, err := s.st.InsertDeduped(r.Context(), id, txs)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.sync.Reclassify(r.Context()); err != nil {
		s.fail(w, r, err)
		return
	}
	first, last := rows[0].BookingDate, rows[0].BookingDate
	for _, row := range rows {
		if row.BookingDate.Before(first) {
			first = row.BookingDate
		}
		if row.BookingDate.After(last) {
			last = row.BookingDate
		}
	}
	writeJSON(w, 200, map[string]any{"added": added, "skipped": skipped, "from": first.Format("2006-01-02"), "to": last.Format("2006-01-02")})
}
