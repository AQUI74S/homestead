package classify

import (
	"regexp"
	"strings"
)

// Txn is the classifier's view of a transaction.
type Txn struct {
	ID               int64
	AmountCents      int64 // negative = expense
	Counterparty     string
	CounterpartyIBAN string
	Remittance       string
	BankCode         string
	Merchant         string // set by Classify
	MerchantKey      string // set by Classify
	Slug             string // current or new category
	Source           string // auto | rule | manual
	Reason           string
	Book             string // book of the own account: haushalt | verwaltung
}

// Rule is a user rule: substring in a field -> category.
type Rule struct {
	Field   string // merchant | counterparty | iban | remittance
	Pattern string
	Slug    string
}

var reMatchClean = regexp.MustCompile(`[^a-z0-9&+]+`)

// normText prepares text for matching: lowercase, umlauts spelled out,
// special characters turned into spaces, wrapped in spaces.
func normText(s string) string {
	s = strings.ToLower(s)
	s = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss").Replace(s)
	s = reMatchClean.ReplaceAllString(s, " ")
	return " " + strings.Join(strings.Fields(s), " ") + " "
}

// containsPattern: short patterns (< 6 chars) must match at a word start,
// so that e.g. "obi" does not hit "mobilfunk". Longer ones may match mid-word.
func containsPattern(text, pattern string) bool {
	p := strings.TrimSpace(normText(pattern))
	if p == "" {
		return false
	}
	if len(p) < 6 || strings.HasSuffix(pattern, " ") {
		if strings.HasSuffix(pattern, " ") {
			return strings.Contains(text, " "+p+" ")
		}
		return strings.Contains(text, " "+p)
	}
	return strings.Contains(text, p)
}

// MatchMerchant finds the category for a merchant/payee name.
func MatchMerchant(name string) (slug string, abo bool, ok bool) {
	t := normText(name)
	for _, m := range merchants {
		for _, p := range m.patterns {
			if containsPattern(t, p) {
				return m.slug, m.abo, true
			}
		}
	}
	return "", false, false
}

func matchKeywords(text string, rules []keywordRule) (string, string, bool) {
	t := normText(text)
	for _, r := range rules {
		for _, w := range r.words {
			if containsPattern(t, w) {
				return r.slug, strings.TrimSpace(w), true
			}
		}
	}
	return "", "", false
}

// Context holds what classification needs besides the transaction.
type Context struct {
	OwnIBANs map[string]bool   // IBANs of own accounts (no spaces, uppercase)
	IBANBook map[string]string // IBAN -> book (haushalt | verwaltung)
	OwnNames []string          // account holder names (normalized), e.g. "max mustermann"
	Rules    []Rule
}

func cleanIBAN(s string) string { return strings.ToUpper(strings.ReplaceAll(s, " ", "")) }

// Classify sets Merchant, MerchantKey and – except for manually categorized transactions –
// Slug, Source and Reason. Recurring patterns refine the result afterwards (see Refine).
func Classify(t *Txn, ctx Context) {
	t.Merchant, t.MerchantKey = Merchant(t.Counterparty, t.Remittance)
	if GenericCounterparty(t.Counterparty) && t.AmountCents < 0 {
		if strings.Contains(strings.ToUpper(t.BankCode), "CWDL") {
			t.Merchant, t.MerchantKey = "Geldautomat", "geldautomat"
		} else if _, _, ok := matchKeywords(t.Remittance, []keywordRule{cashKeywords}); ok {
			t.Merchant, t.MerchantKey = "Geldautomat", "geldautomat"
		}
	}
	if t.Source == "manual" {
		return
	}
	t.Source = "auto"
	credit := t.AmountCents > 0
	lowMerchant := strings.ToLower(t.Merchant)
	lowCP := strings.ToLower(t.Counterparty)
	lowRem := strings.ToLower(t.Remittance)
	iban := cleanIBAN(t.CounterpartyIBAN)

	// 1. User rules
	for _, r := range ctx.Rules {
		p := strings.ToLower(strings.TrimSpace(r.Pattern))
		if p == "" {
			continue
		}
		var hit bool
		switch r.Field {
		case "merchant":
			hit = t.MerchantKey == p || strings.Contains(lowMerchant, p)
		case "counterparty":
			hit = strings.Contains(lowCP, p)
		case "iban":
			hit = iban != "" && iban == cleanIBAN(p)
		case "remittance":
			hit = strings.Contains(lowRem, p)
		}
		if hit {
			t.Slug, t.Source, t.Reason = r.Slug, "rule", "Eigene Regel: "+r.Field+" enthält „"+r.Pattern+"“"
			return
		}
	}

	// 2. Transfer between own accounts
	if iban != "" && ctx.OwnIBANs[iban] {
		other := ctx.IBANBook[iban]
		if t.Book != "verwaltung" && other == "verwaltung" {
			// Money between rental account and private account counts in the household book
			if credit {
				t.Slug, t.Reason = "vermietung", "Überweisung vom Mietkonto"
			} else {
				t.Slug, t.Reason = "zuschuss-vermietung", "Überweisung aufs Mietkonto"
			}
			return
		}
		t.Slug, t.Reason = "umbuchung", "Gegenkonto ist ein eigenes Konto"
		return
	}

	// 2b. Transfer to/from own name on an unconnected account (e.g. savings account)
	if ncp := strings.TrimSpace(normText(t.Counterparty)); ncp != "" {
		for _, n := range ctx.OwnNames {
			if n != "" && (ncp == n || strings.HasPrefix(ncp, n+" ")) {
				t.Slug, t.Reason = "sparen", "Überweisung auf eigenen Namen (Konto nicht verbunden)"
				return
			}
		}
	}

	if credit {
		// 3a. Income: keywords in remittance or sender
		if slug, w, ok := matchKeywords(t.Counterparty+" "+t.Remittance, creditKeywords); ok {
			t.Slug, t.Reason = slug, "Stichwort „"+w+"“"
			return
		}
		// 3b. Credit from a known merchant = refund
		if _, _, ok := MatchMerchant(t.Merchant + " " + t.Counterparty); ok {
			t.Slug, t.Reason = "erstattung", "Gutschrift von Händler "+t.Merchant
			return
		}
		t.Slug, t.Reason = "einnahmen-sonst", "Eingang ohne eindeutiges Merkmal"
		return
	}

	// 4. Expenses: merchant name (for Amazon/Apple/Google the remittance counts too,
	// since it contains e.g. "Prime" or "iCloud")
	mtext := t.Merchant + " " + t.Counterparty
	if strings.Contains(lowCP, "amazon") || strings.Contains(lowCP, "apple") || strings.Contains(lowCP, "google") {
		mtext = t.Remittance + " " + mtext
	}
	if slug, _, ok := MatchMerchant(mtext); ok {
		if slug == "strom" || slug == "gas" || slug == "wasser" {
			if s2, w, ok := matchKeywords(t.Remittance, utilityKeywords); ok {
				t.Slug, t.Reason = s2, "Versorger "+t.Merchant+", Stichwort „"+w+"“"
				return
			}
		}
		t.Slug, t.Reason = slug, "Bekannter Händler: "+t.Merchant
		return
	}
	// 5. Keywords in remittance
	if slug, w, ok := matchKeywords(t.Remittance, debitKeywords); ok {
		t.Slug, t.Reason = slug, "Stichwort „"+w+"“ im Verwendungszweck"
		return
	}
	// 6. Bank transaction code for cash
	if strings.Contains(strings.ToUpper(t.BankCode), "CWDL") {
		t.Slug, t.Reason = "bargeld", "Bargeldauszahlung laut Bankcode"
		return
	}
	t.Slug, t.Reason = "sonstiges", "Nicht zugeordnet"
}

// IsAboMerchant reports whether the merchant is typically a subscription.
func IsAboMerchant(name string) bool {
	_, abo, ok := MatchMerchant(name)
	return ok && abo
}

// OwnNamesFrom filters account names that look like personal names
// (some banks return the holder's name as the account name).
func OwnNamesFrom(names []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, n := range names {
		t := strings.TrimSpace(normText(n))
		words := strings.Fields(t)
		if len(words) < 2 || len(words) > 4 || seen[t] {
			continue
		}
		skip := false
		for _, w := range []string{"konto", "giro", "spar", "tagesgeld", "depot", "karte", "card", "gemeinschaft", "plus", "komfort", "online", "classic", "premium", "basis", "direkt", "business", "privat"} {
			if strings.Contains(t, w) {
				skip = true
			}
		}
		for _, r := range t {
			if r >= '0' && r <= '9' {
				skip = true
			}
		}
		if !skip {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// NormName normalizes a name the way matching against payee names expects it.
func NormName(n string) string { return strings.TrimSpace(normText(n)) }
