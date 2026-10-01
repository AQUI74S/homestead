package classify

import (
	"regexp"
	"strings"

	"github.com/AQUI74S/homestead/internal/domain"
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
	Source           domain.Source
	Reason           string
	Book             domain.Book // book of the own account
}

// Rule is a user rule: substring in a field -> category.
type Rule struct {
	Field   domain.RuleField
	Pattern string
	Slug    string
}

var reMatchClean = regexp.MustCompile(`[^a-z0-9&+]+`)

const (
	// bankCodeCash is the ISO 20022 bank transaction code of a cash withdrawal.
	bankCodeCash = "CWDL"
	// cashMerchant names withdrawals whose payee is only a bank placeholder.
	cashMerchant, cashMerchantKey = "Geldautomat", "geldautomat"
)

// marketplaces sell many kinds of things; for them the remittance text (e.g.
// "Prime", "iCloud") decides the category rather than the payee name.
var marketplaces = []string{"amazon", "apple", "google"}

// collectorKeywords: payees that collect for several categories, by the category
// of their name. For them the remittance text decides; without a hint the
// category of the name stays. Energy suppliers often deliver several utilities,
// municipalities collect taxes but also kindergarten, water and waste fees.
var collectorKeywords = map[string][]keywordRule{
	"strom": utilityKeywords, "gas": utilityKeywords, "wasser": utilityKeywords,
	"steuern": authorityKeywords,
}

// accountNameWords mark an account name as a product name rather than a person.
var accountNameWords = []string{"konto", "giro", "spar", "tagesgeld", "depot", "karte", "card", "gemeinschaft", "plus", "komfort",
	"online", "classic", "premium", "basis", "direkt", "business", "privat"}

// normText prepares text for matching: lowercase, umlauts spelled out,
// special characters turned into spaces, wrapped in spaces.
func normText(s string) string {
	s = reMatchClean.ReplaceAllString(domain.Fold(s), " ")
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
	OwnIBANs map[string]bool        // IBANs of own accounts (no spaces, uppercase)
	IBANBook map[string]domain.Book // IBAN -> book of the own account
	OwnNames []string               // account holder names (normalized), e.g. "max mustermann"
	Rules    []Rule
}

// Classify sets Merchant, MerchantKey and – except for manually categorized transactions –
// Slug, Source and Reason. Recurring patterns refine the result afterwards (see Refine).
func Classify(t *Txn, ctx Context) {
	t.Merchant, t.MerchantKey = Merchant(t.Counterparty, t.Remittance)
	if GenericCounterparty(t.Counterparty) && t.AmountCents < 0 {
		if isCashCode(t.BankCode) {
			t.Merchant, t.MerchantKey = cashMerchant, cashMerchantKey
		} else if _, _, ok := matchKeywords(t.Remittance, []keywordRule{cashKeywords}); ok {
			t.Merchant, t.MerchantKey = cashMerchant, cashMerchantKey
		}
	}
	if t.Source == domain.SourceManual {
		return
	}
	t.Source = domain.SourceAuto
	credit := t.AmountCents > 0
	lowMerchant := strings.ToLower(t.Merchant)
	lowCP := strings.ToLower(t.Counterparty)
	lowRem := strings.ToLower(t.Remittance)
	iban := domain.NormIBAN(t.CounterpartyIBAN)

	// 1. User rules
	for _, r := range ctx.Rules {
		p := strings.ToLower(strings.TrimSpace(r.Pattern))
		if p == "" {
			continue
		}
		var hit bool
		switch r.Field {
		case domain.RuleMerchant:
			hit = t.MerchantKey == p || strings.Contains(lowMerchant, p)
		case domain.RuleCounterparty:
			hit = strings.Contains(lowCP, p)
		case domain.RuleIBAN:
			hit = iban != "" && iban == domain.NormIBAN(p)
		case domain.RuleRemittance:
			hit = strings.Contains(lowRem, p)
		}
		if hit {
			t.Slug, t.Source, t.Reason = r.Slug, domain.SourceRule, "Eigene Regel: "+string(r.Field)+" enthält „"+r.Pattern+"“"
			return
		}
	}

	// 2. Transfer between own accounts
	if iban != "" && ctx.OwnIBANs[iban] {
		other := ctx.IBANBook[iban]
		if t.Book != domain.BookProperty && other == domain.BookProperty {
			// Money between rental account and private account counts in the household book
			if credit {
				t.Slug, t.Reason = domain.SlugRental, "Überweisung vom Mietkonto"
			} else {
				t.Slug, t.Reason = domain.SlugRentalSubsidy, "Überweisung aufs Mietkonto"
			}
			return
		}
		t.Slug, t.Reason = domain.SlugTransfer, "Gegenkonto ist ein eigenes Konto"
		return
	}

	// 2b. Transfer to/from own name on an unconnected account (e.g. savings account)
	if ncp := strings.TrimSpace(normText(t.Counterparty)); ncp != "" {
		for _, n := range ctx.OwnNames {
			if n != "" && (ncp == n || strings.HasPrefix(ncp, n+" ")) {
				t.Slug, t.Reason = domain.SlugSavings, "Überweisung auf eigenen Namen (Konto nicht verbunden)"
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
			t.Slug, t.Reason = domain.SlugRefund, "Gutschrift von Händler "+t.Merchant
			return
		}
		t.Slug, t.Reason = domain.SlugOtherIncome, "Eingang ohne eindeutiges Merkmal"
		return
	}

	// 4. Bank fees. The payee is often the bank itself, which may also be a lender
	// (Targobank, Santander), so this comes before the merchant name.
	if slug, w, ok := matchKeywords(t.Counterparty+" "+t.Remittance, []keywordRule{feeKeywords}); ok {
		t.Slug, t.Reason = slug, "Stichwort „"+w+"“ (Bankentgelt)"
		return
	}
	if isFeeCode(t.BankCode) {
		t.Slug, t.Reason = domain.SlugBankFees, "Bankentgelt laut Bankcode"
		return
	}

	// 5. Expenses: merchant name (for Amazon/Apple/Google the remittance counts too,
	// since it contains e.g. "Prime" or "iCloud")
	mtext := t.Merchant + " " + t.Counterparty
	for _, m := range marketplaces {
		if strings.Contains(lowCP, m) {
			mtext = t.Remittance + " " + mtext
			break
		}
	}
	if slug, _, ok := MatchMerchant(mtext); ok {
		if s2, w, ok := matchKeywords(t.Remittance, collectorKeywords[slug]); ok {
			t.Slug, t.Reason = s2, t.Merchant+", Stichwort „"+w+"“ im Verwendungszweck"
			return
		}
		t.Slug, t.Reason = slug, "Bekannter Händler: "+t.Merchant
		return
	}
	// 6. Keywords in remittance
	if slug, w, ok := matchKeywords(t.Remittance, debitKeywords); ok {
		t.Slug, t.Reason = slug, "Stichwort „"+w+"“ im Verwendungszweck"
		return
	}
	// 7. Bank transaction code for cash
	if isCashCode(t.BankCode) {
		t.Slug, t.Reason = domain.SlugCash, "Bargeldauszahlung laut Bankcode"
		return
	}
	t.Slug, t.Reason = domain.SlugOther, "Nicht zugeordnet"
}

func isCashCode(code string) bool { return strings.Contains(strings.ToUpper(code), bankCodeCash) }

// feeCodes are ISO 20022 bank transaction sub-families of charges the bank books itself.
var feeCodes = map[string]bool{"CHRG": true, "FEES": true}

func isFeeCode(code string) bool {
	for _, f := range strings.Fields(strings.ToUpper(code)) {
		if feeCodes[f] {
			return true
		}
	}
	return false
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
		for _, w := range accountNameWords {
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
