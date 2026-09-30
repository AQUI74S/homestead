package enablebanking

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/AQUI74S/homestead/internal/domain"
)

// apiDate is the date format of the Enable Banking API (ISO 8601 day).
const apiDate = "2006-01-02"

// Normalized is a transaction in the format stored in the database.
type Normalized struct {
	ExtID            string
	BookingDate      time.Time
	ValueDate        *time.Time
	AmountCents      int64 // negative = expense
	Currency         string
	Counterparty     string
	CounterpartyIBAN string
	Remittance       string
	BankCode         string
	Pending          bool
}

// ParseCents converts "1234.5" or "1234,50" to cents without floating-point rounding errors.
func ParseCents(s string) (int64, error) {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", "."))
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimLeft(s, "+-")
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" {
		whole = "0"
	}
	frac = (frac + "00")[:2]
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("Betrag %q: %w", s, err)
	}
	f, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("Betrag %q: %w", s, err)
	}
	c := w*100 + f
	if neg {
		c = -c
	}
	return c, nil
}

// Normalize converts an API transaction to the internal format. seen counts identical
// transactions without a bank ID so two identical card payments on the same day don't merge.
func Normalize(t Transaction, seen map[string]int) (Normalized, error) {
	var n Normalized
	cents, err := ParseCents(t.TransactionAmount.Amount)
	if err != nil {
		return n, err
	}
	if cents < 0 {
		cents = -cents
	}
	debit := strings.EqualFold(t.CreditDebitIndicator, "DBIT")
	if debit {
		cents = -cents
	}
	n.AmountCents = cents
	n.Currency = t.TransactionAmount.Currency
	if n.Currency == "" {
		n.Currency = "EUR"
	}
	n.Pending = strings.EqualFold(t.Status, "PDNG")

	date := firstNonEmpty(t.BookingDate, t.ValueDate, t.TransactionDate)
	if n.BookingDate, err = time.Parse(apiDate, date); err != nil {
		return n, fmt.Errorf("Buchungsdatum %q: %w", date, err)
	}
	if t.ValueDate != "" {
		if vd, err := time.Parse(apiDate, t.ValueDate); err == nil {
			n.ValueDate = &vd
		}
	}

	var party *Party
	var acct *AccountID
	if debit {
		party, acct = t.Creditor, t.CreditorAccount
	} else {
		party, acct = t.Debtor, t.DebtorAccount
	}
	if party != nil {
		n.Counterparty = strings.TrimSpace(party.Name)
	}
	if acct != nil {
		n.CounterpartyIBAN = domain.NormIBAN(acct.IBAN)
	}
	n.Remittance = strings.TrimSpace(strings.Join(t.RemittanceInformation, " "))
	if n.Remittance == "" {
		n.Remittance = strings.TrimSpace(t.Note)
	}
	if bc := t.BankTransactionCode; bc != nil {
		n.BankCode = strings.Trim(strings.Join([]string{bc.Code, bc.SubCode, bc.Description}, " "), " ")
	}

	n.ExtID = firstNonEmpty(t.EntryReference, t.TransactionID)
	if n.ExtID == "" {
		h := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s|%s|%s", date, n.AmountCents, n.Counterparty, n.CounterpartyIBAN, n.Remittance)))
		base := "h:" + hex.EncodeToString(h[:12])
		seen[base]++
		n.ExtID = fmt.Sprintf("%s:%d", base, seen[base])
	}
	return n, nil
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			return strings.TrimSpace(x)
		}
	}
	return ""
}
