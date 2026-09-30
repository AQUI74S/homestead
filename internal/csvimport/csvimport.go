// Package csvimport reads transaction exports from German banks (Sparkasse CSV-CAMT, Volksbank,
// DKB, ING, Commerzbank, comdirect, Postbank, etc.). Columns are detected by their headers.
package csvimport

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	eb "github.com/AQUI74S/homestead/internal/enablebanking"

	"github.com/AQUI74S/homestead/internal/domain"
)

type Row struct {
	BookingDate  time.Time
	AmountCents  int64
	Counterparty string
	IBAN         string
	Remittance   string
}

// Windows-1252 special characters in the range 0x80–0x9F
var cp1252 = map[byte]rune{0x80: '€', 0x82: '‚', 0x84: '„', 0x85: '…', 0x91: '‘', 0x92: '’', 0x93: '“', 0x94: '”', 0x96: '–', 0x97: '—'}

func toUTF8(b []byte) string {
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	if utf8.Valid(b) {
		return string(b)
	}
	var sb strings.Builder
	for _, c := range b {
		if r, ok := cp1252[c]; ok {
			sb.WriteRune(r)
		} else {
			sb.WriteRune(rune(c)) // Latin-1
		}
	}
	return sb.String()
}

func norm(h string) string {
	h = strings.ToLower(strings.TrimSpace(strings.Trim(h, "\"")))
	return strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss", "*", "", "  ", " ").Replace(h)
}

type cols struct{ date, value, amount, soll, haben, cp, payee, payer, iban, rem, text int }

func findCols(header []string) (cols, int) {
	c := cols{-1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1}
	score := 0
	for i, raw := range header {
		h := norm(raw)
		switch {
		case c.date < 0 && (strings.HasPrefix(h, "buchungstag") || strings.HasPrefix(h, "buchungsdatum") || h == "buchung" || h == "datum"):
			c.date = i
			score++
		case strings.HasPrefix(h, "valuta") || strings.HasPrefix(h, "wertstellung"):
			c.value = i
		case c.amount < 0 && (h == "betrag" || strings.HasPrefix(h, "betrag (") || strings.HasPrefix(h, "betrag in") || strings.HasPrefix(h, "umsatz in") || h == "umsatz" || h == "betrag eur"):
			c.amount = i
			score++
		case h == "soll" || strings.HasPrefix(h, "soll ("):
			c.soll = i
		case h == "haben" || strings.HasPrefix(h, "haben ("):
			c.haben = i
		case c.cp < 0 && (strings.Contains(h, "beguenstigter/zahlungspflichtiger") || strings.Contains(h, "auftraggeber / beguenstigter") ||
			strings.Contains(h, "auftraggeber/empfaenger") || strings.Contains(h, "auftraggeber / empfaenger") ||
			strings.Contains(h, "name zahlungsbeteiligter") || h == "name" || strings.Contains(h, "gegenseite")):
			c.cp = i
			score++
		case c.payee < 0 && (strings.Contains(h, "zahlungsempfaenger") || h == "empfaenger" || h == "beguenstigter"):
			c.payee = i
			score++
		case c.payer < 0 && (strings.Contains(h, "zahlungspflichtige") || h == "auftraggeber"):
			c.payer = i
		case c.iban < 0 && strings.Contains(h, "iban") && !strings.Contains(h, "auftragskonto") && !strings.Contains(h, "kontoinhaber"):
			c.iban = i
		case strings.HasPrefix(h, "verwendungszweck"):
			c.rem = i
		case h == "buchungstext" || h == "umsatzart" || h == "vorgang" || h == "buchungsart":
			if c.text < 0 {
				c.text = i
			}
		}
	}
	if c.amount < 0 && c.soll >= 0 {
		score++
	}
	return c, score
}

func parseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, f := range []string{"02.01.2006", "02.01.06", "2006-01-02", "2.1.2006"} {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("Datum %q nicht lesbar", s)
}

func parseAmount(s string) (int64, error) {
	s = strings.TrimSpace(strings.NewReplacer("€", "", "EUR", "", " ", "", " ", "").Replace(s))
	if s == "" {
		return 0, nil
	}
	if strings.Contains(s, ",") {
		s = strings.ReplaceAll(s, ".", "")
	}
	neg := strings.HasSuffix(s, "-") // some banks: "12,34-"
	s = strings.TrimSuffix(s, "-")
	c, err := eb.ParseCents(s)
	if neg {
		c = -abs(c)
	}
	return c, err
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// Parse reads a CSV file. Preamble lines (account info at DKB/ING) are skipped.
func Parse(data []byte) ([]Row, error) {
	text := strings.ReplaceAll(toUTF8(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	// find header line and separator
	hdrIdx, sep := -1, ';'
	var c cols
	for i, l := range lines {
		if i > 40 {
			break
		}
		for _, d := range []rune{';', ',', '\t'} {
			if !strings.ContainsRune(l, d) {
				continue
			}
			r := csv.NewReader(strings.NewReader(l))
			r.Comma, r.LazyQuotes = d, true
			rec, err := r.Read()
			if err != nil {
				continue
			}
			if cc, score := findCols(rec); score >= 2 && cc.date >= 0 && (cc.amount >= 0 || cc.soll >= 0) {
				hdrIdx, sep, c = i, d, cc
				break
			}
		}
		if hdrIdx >= 0 {
			break
		}
	}
	if hdrIdx < 0 {
		return nil, errors.New("Keine Spaltenüberschriften erkannt (erwartet u. a. Buchungstag, Betrag, Empfänger)")
	}
	r := csv.NewReader(strings.NewReader(strings.Join(lines[hdrIdx+1:], "\n")))
	r.Comma, r.LazyQuotes, r.FieldsPerRecord = sep, true, -1
	get := func(rec []string, i int) string {
		if i < 0 || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}
	var out []Row
	for n := 1; ; n++ {
		rec, err := r.Read()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, fmt.Errorf("Zeile %d: %w", hdrIdx+1+n, err)
		}
		if strings.TrimSpace(strings.Join(rec, "")) == "" {
			continue
		}
		d, err := parseDate(get(rec, c.date))
		if err != nil {
			continue // total or footer lines
		}
		var amt int64
		if c.amount >= 0 {
			amt, err = parseAmount(get(rec, c.amount))
		} else {
			var soll, haben int64
			soll, err = parseAmount(get(rec, c.soll))
			if err == nil {
				haben, err = parseAmount(get(rec, c.haben))
			}
			amt = haben - abs(soll)
		}
		if err != nil {
			return nil, fmt.Errorf("Zeile %d: %w", hdrIdx+1+n, err)
		}
		rem := get(rec, c.rem)
		if t := get(rec, c.text); t != "" && c.rem < 0 {
			rem = t
		}
		cp := get(rec, c.cp)
		if cp == "" { // separate columns (DKB): payee for expenses, payer for income
			if amt < 0 {
				cp = get(rec, c.payee)
			} else {
				cp = get(rec, c.payer)
			}
		}
		if cp == "" && c.text >= 0 && c.rem >= 0 {
			// Commerzbank & co.: payee is in the booking text
			cp = get(rec, c.text)
		}
		out = append(out, Row{BookingDate: d, AmountCents: amt, Counterparty: cp,
			IBAN: domain.NormIBAN(get(rec, c.iban)), Remittance: rem})
	}
	if len(out) == 0 {
		return nil, errors.New("Keine Umsätze in der Datei gefunden")
	}
	return out, nil
}
