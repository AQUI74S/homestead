// Package classify assigns categories to bank transactions and detects
// recurring payments (subscriptions, fixed costs, income).
package classify

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/AQUI74S/homestead/internal/domain"
)

var (
	reNonAlnum   = regexp.MustCompile(`[^a-z0-9äöüß&+]+`)
	reDigits     = regexp.MustCompile(`\b[0-9][0-9./-]*\b`)
	reSpaces     = regexp.MustCompile(`\s+`)
	reEinkaufBei = regexp.MustCompile(`(?i)ihr einkauf bei\s+([^,/]+)`)
	// Securities settlements carry a unique order number: "WP-ABRECHNUNG 0400000000001Kauf ISIN …"
	reWPAbrechnung = regexp.MustCompile(`(?i)^\s*(wp-?abrechnung|wertpapierabrechnung|wertpapier-abrechnung)\b`)
	// Periodic account closings: "Saldo der Abschlussposten …", "Kontoabschluss 3. Quartal"
	reAccountClosing = regexp.MustCompile(`(?i)abschlussposten|kontoabschluss|rechnungsabschluss`)
	rePPDot          = regexp.MustCompile(`(?i)\bpp\.\d+\.pp\s*\.\s*([^,/]+)`)
	// Structured SEPA fields some banks put into the remittance text, often empty:
	// "mandatereference:,creditorid:,remittanceinformation: Zins/Dividende …"
	// or "EREF+… MREF+… SVWZ+Miete Oktober".
	reSepaFields = regexp.MustCompile(`(?i)\b(mandate ?reference|mandate ?id|creditor ?id|end ?to ?end ?id|end-to-end-id)\s*:\s*[^,]*,?|\b(EREF|MREF|CRED|KREF|ABWA|ABWE|IBAN|BIC)\+\S*|\b(remittance ?information|SVWZ)\s*[:+]\s*`)
)

// CleanRemittance removes structured SEPA field labels (and empty fields) from a
// remittance text, leaving what a person would read.
func CleanRemittance(s string) string {
	s = reSepaFields.ReplaceAllString(s, " ")
	return strings.TrimSpace(reSpaces.ReplaceAllString(strings.Trim(s, " ,;"), " "))
}

// Legal forms and filler words that don't matter for merchant detection.
var stopWords = map[string]bool{
	"gmbh": true, "mbh": true, "ag": true, "se": true, "kg": true, "co": true, "ohg": true, "ek": true,
	"e.k": true, "ug": true, "haftungsbeschränkt": true, "bv": true, "b.v": true, "sarl": true, "s.a.r.l": true,
	"sa": true, "ltd": true, "limited": true, "inc": true, "llc": true, "plc": true, "sagt": true, "danke": true,
	"europe": true, "eu": true, "deutschland": true, "germany": true, "de": true, "international": true,
	"services": true, "service": true, "zweigniederlassung": true, "holding": true, "und": true, "the": true,
	"markt": true, "filiale": true, "fil": true, "nl": true, "niederlassung": true,
}

// Payment providers behind which the actual merchant appears in the remittance text.
var paymentProviders = []string{"paypal", "klarna", "sofort", "stripe", "adyen", "mollie", "unzer", "computop"}

// Merchant returns a readable merchant name and a normalized key
// used to group identical merchants.
func Merchant(counterparty, remittance string) (display, key string) {
	cp := strings.TrimSpace(counterparty)
	if i := strings.Index(cp, "//"); i >= 0 { // card payment: "REWE Markt GmbH//Berlin/DE"
		cp = strings.TrimSpace(cp[:i])
	}
	lcp := strings.ToLower(cp)

	for _, p := range paymentProviders {
		if strings.Contains(lcp, p) {
			if m := reEinkaufBei.FindStringSubmatch(remittance); m != nil {
				cp = strings.TrimSpace(m[1])
			} else if m := rePPDot.FindStringSubmatch(remittance); m != nil {
				cp = strings.TrimSpace(m[1])
			} else {
				cp = titleCase(p)
			}
			break
		}
	}
	if GenericCounterparty(cp) { // some banks only provide the remittance text for card payments
		cp = CleanRemittance(remittance)
		if i := strings.Index(cp, "//"); i >= 0 {
			cp = cp[:i]
		}
		if len(cp) > 40 {
			cp = cp[:40]
		}
	}
	if reWPAbrechnung.MatchString(cp) || cp == "" && reWPAbrechnung.MatchString(remittance) {
		return "Wertpapier-Abrechnung", "wertpapier abrechnung"
	}
	if reAccountClosing.MatchString(cp) {
		return "Kontoabschluss", "kontoabschluss"
	}
	key = NormalizeKey(cp)
	display = prettyName(cp)
	return display, key
}

// NormalizeKey normalizes a name: lowercase, without digits, legal forms and punctuation,
// at most three words.
func NormalizeKey(s string) string {
	s = reDigits.ReplaceAllString(domain.Fold(s), " ")
	s = reNonAlnum.ReplaceAllString(s, " ")
	var out []string
	for _, w := range strings.Fields(s) {
		if stopWords[w] || len(w) < 2 && w != "&" {
			continue
		}
		if w == "&" {
			continue
		}
		out = append(out, w)
		if len(out) == 3 {
			break
		}
	}
	return strings.Join(out, " ")
}

func prettyName(s string) string {
	s = reSpaces.ReplaceAllString(strings.TrimSpace(s), " ")
	if s == "" {
		return "Unbekannt"
	}
	// Only convert all-caps; leave mixed case (e.g. "eBay") as is.
	if strings.ToUpper(s) == s {
		return titleCase(strings.ToLower(s))
	}
	return s
}

func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if strings.Contains(w, ".") { // keep abbreviations like B.V. or S.A. uppercase
			words[i] = strings.ToUpper(w)
			continue
		}
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}

// Placeholders some banks enter as payee instead of the merchant
// (the real merchant is then in the remittance text).
var genericCounterparties = []string{
	"abrechnung karte", "kartenzahlung", "karte ", "girocard", "debitkarte", "debit karte", "visa debit",
	"mastercard", "maestro", "kartenumsatz", "kartentransaktion", "ec-karte", "ec karte", "pos ", "sepa-lastschrift",
	"lastschrift", "abrechnung",
}

// GenericCounterparty reports whether the payee name is just a bank placeholder.
func GenericCounterparty(cp string) bool {
	l := strings.ToLower(strings.TrimSpace(cp))
	if l == "" {
		return true
	}
	for _, g := range genericCounterparties {
		gt := strings.TrimSpace(g)
		if l == gt || (strings.HasSuffix(g, " ") && strings.HasPrefix(l, g)) || strings.HasPrefix(l, gt+" ") && len(l) <= len(gt)+12 {
			return true
		}
	}
	return false
}
