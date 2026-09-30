package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/AQUI74S/homestead/internal/budget"
	"github.com/AQUI74S/homestead/internal/classify"
	"github.com/AQUI74S/homestead/internal/csvimport"
	"github.com/AQUI74S/homestead/internal/domain"
	"github.com/AQUI74S/homestead/internal/store"
)

// bookAll in the "book" query parameter lists the transactions of both books.
const bookAll = "alle"

func (s *Server) transactions(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	f := store.TxnFilter{CategoryID: queryInt64(r, "category_id"), Group: domain.Group(q.Get("group")),
		AccountID: queryInt64(r, "account_id"), Search: q.Get("q"), RecurringID: queryInt64(r, "recurring_id"), Limit: limit,
		Book: domain.Book(q.Get("book")), LeaseID: queryInt64(r, "lease_id"), PropertyID: queryInt64(r, "property_id"),
		Uncategorized: q.Get("uncategorized") == "1"}
	switch f.Book {
	case "":
		f.Book = domain.BookHousehold
	case bookAll:
		f.Book = ""
	}
	if month := q.Get("month"); month != "" {
		p, err := budget.LoadPeriods(r.Context(), s.st)
		if err != nil {
			return err
		}
		if f.From, f.To, err = p.Range(month); err != nil {
			return err
		}
	}
	txs, err := s.st.Transactions(r.Context(), f)
	if err != nil {
		return err
	}
	for i := range txs { // show the text without empty SEPA field labels
		txs[i].Remittance = classify.CleanRemittance(txs[i].Remittance)
	}
	return reply(w, txs)
}

func (s *Server) patchTransaction(w http.ResponseWriter, r *http.Request) error {
	id, ok := pathID(r)
	var in struct {
		CategoryID int64   `json:"category_id"`
		Note       *string `json:"note"`
		Rule       bool    `json:"rule"`  // create a rule for this merchant
		Reset      bool    `json:"reset"` // revert to automatic categorization
	}
	if !ok || decode(r, &in) != nil {
		return badRequest("Ungültige Anfrage")
	}
	ctx := r.Context()
	switch {
	case in.Reset:
		if err := s.st.ResetTransactionCategory(ctx, id); err != nil {
			return err
		}
	case in.CategoryID <= 0:
		return badRequest("Kategorie wählen")
	default:
		if err := s.st.SetTransactionCategory(ctx, id, in.CategoryID, in.Note); err != nil {
			return err
		}
		if in.Rule {
			if err := s.ruleFromTransaction(r, id, in.CategoryID); err != nil {
				return err
			}
		}
	}
	if err := s.sync.Reclassify(ctx); err != nil {
		return err
	}
	return okReply(w)
}

// ruleFromTransaction creates a rule "this merchant (or IBAN) -> category" and hands
// earlier hand choices with the same category over to it.
func (s *Server) ruleFromTransaction(r *http.Request, txnID, categoryID int64) error {
	ctx := r.Context()
	t, err := s.st.TransactionByID(ctx, txnID)
	if err != nil {
		return err
	}
	field, pattern := domain.RuleMerchant, t.MerchantKey
	if pattern == "" && t.CounterpartyIBAN != "" {
		field, pattern = domain.RuleIBAN, t.CounterpartyIBAN
	}
	if pattern == "" {
		return badRequest("Für diesen Umsatz lässt sich keine Regel ableiten (kein Händlername)")
	}
	if err := s.st.UpsertRule(ctx, field, pattern, categoryID); err != nil {
		return err
	}
	return s.st.ReleaseToRule(ctx, field, pattern, categoryID)
}

func (s *Server) rules(w http.ResponseWriter, r *http.Request) error {
	rules, err := s.st.Rules(r.Context())
	if err != nil {
		return err
	}
	return reply(w, rules)
}

func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request) error {
	id, err := requireID(r)
	if err != nil {
		return err
	}
	if err := s.st.DeleteRule(r.Context(), id); err != nil {
		return err
	}
	if err := s.sync.Reclassify(r.Context()); err != nil {
		return err
	}
	return okReply(w)
}

type importResponse struct {
	Added   int    `json:"added"`
	Skipped int    `json:"skipped"`
	From    string `json:"from"`
	To      string `json:"to"`
}

// importCSV imports an account statement exported from online banking.
func (s *Server) importCSV(w http.ResponseWriter, r *http.Request) error {
	id, ok := pathID(r)
	if !ok {
		return badRequest("Ungültiges Konto")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBody)
	f, _, err := r.FormFile("file")
	if err != nil {
		return badRequest("CSV-Datei fehlt")
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return badRequest("Datei nicht lesbar")
	}
	rows, err := csvimport.Parse(data)
	if err != nil {
		return badRequest(err.Error())
	}
	added, skipped, err := s.st.InsertDeduped(r.Context(), id, csvTxns(rows))
	if err != nil {
		return err
	}
	if err := s.sync.Reclassify(r.Context()); err != nil {
		return err
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
	return reply(w, importResponse{Added: added, Skipped: skipped, From: first.Format(domain.DateLayout), To: last.Format(domain.DateLayout)})
}

// csvTxns converts imported rows. Each gets a stable ID from its content, so that
// importing the same file twice adds nothing; identical rows are numbered.
func csvTxns(rows []csvimport.Row) []store.NewTxn {
	const hashBytes = 12
	seen := map[string]int{}
	txs := make([]store.NewTxn, 0, len(rows))
	for _, row := range rows {
		h := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s|%s", row.BookingDate.Format(domain.DateLayout), row.AmountCents, row.Counterparty, row.Remittance)))
		base := store.CSVExtIDPrefix + hex.EncodeToString(h[:hashBytes])
		seen[base]++
		txs = append(txs, store.NewTxn{ExtID: fmt.Sprintf("%s:%d", base, seen[base]), BookingDate: row.BookingDate,
			AmountCents: row.AmountCents, Currency: "EUR", Counterparty: row.Counterparty, CounterpartyIBAN: row.IBAN, Remittance: row.Remittance})
	}
	return txs
}
