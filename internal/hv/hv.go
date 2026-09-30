// Package hv contains the property management (Hausverwaltung) logic: assignment of
// rent account (Mietkonto) transactions, rent due vs. paid, utility cost settlement
// (Nebenkostenabrechnung), property overview and deadlines.
package hv

import (
	"sort"
	"strings"
	"time"

	"github.com/AQUI74S/homestead/internal/classify"
	"github.com/AQUI74S/homestead/internal/store"
)

// CostType describes a cost or booking type on the rent account.
type CostType struct {
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	Kind       string `json:"kind"` // einnahme | kosten | neutral
	Umlage     bool   `json:"umlagefaehig"`
	DefaultKey string `json:"default_key"` // flaeche | personen | einheiten
}

var CostTypes = []CostType{
	{"miete", "Miete (inkl. NK-Vorauszahlung)", "einnahme", false, ""},
	{"nk_nachzahlung", "Nebenkosten-Nachzahlung", "einnahme", false, ""},
	{"kaution", "Kaution", "neutral", false, ""},
	{"einnahme_sonst", "Sonstige Einnahme", "einnahme", false, ""},
	{"grundsteuer", "Grundsteuer", "kosten", true, "flaeche"},
	{"wasser", "Wasser", "kosten", true, "personen"},
	{"abwasser", "Abwasser / Kanal", "kosten", true, "personen"},
	{"muell", "Müllabfuhr", "kosten", true, "personen"},
	{"heizung", "Heizung / Warmwasser", "kosten", true, "flaeche"},
	{"strom_allgemein", "Allgemeinstrom", "kosten", true, "flaeche"},
	{"versicherung", "Gebäudeversicherung", "kosten", true, "flaeche"},
	{"hausmeister", "Hausmeister", "kosten", true, "flaeche"},
	{"gartenpflege", "Gartenpflege", "kosten", true, "flaeche"},
	{"reinigung", "Gebäudereinigung", "kosten", true, "flaeche"},
	{"schornsteinfeger", "Schornsteinfeger", "kosten", true, "einheiten"},
	{"strassenreinigung", "Straßenreinigung / Winterdienst", "kosten", true, "flaeche"},
	{"kabel", "Kabel-TV / Antenne", "kosten", true, "einheiten"},
	{"sonstige_bk", "Sonstige Betriebskosten", "kosten", true, "flaeche"},
	{"hausgeld", "Hausgeld (WEG)", "kosten", false, ""},
	{"reparatur", "Reparaturen & Instandhaltung", "kosten", false, ""},
	{"verwaltung", "Verwaltung & Bank", "kosten", false, ""},
	{"kredit", "Darlehen (Zins + Tilgung)", "kosten", false, ""},
	{"sonstiges", "Sonstige Kosten", "kosten", false, ""},
	{"entnahme", "Entnahme / Einlage (eigene Konten)", "neutral", false, ""},
}

var costTypeBySlug = func() map[string]CostType {
	m := map[string]CostType{}
	for _, c := range CostTypes {
		m[c.Slug] = c
	}
	return m
}()

func TypeOf(slug string) (CostType, bool) { c, ok := costTypeBySlug[slug]; return c, ok }

// Keywords for cost types (counterparty + remittance info, normalized).
var costKeywords = []struct {
	slug  string
	words []string
}{
	{"hausgeld", []string{"hausgeld", "wohnungseigentuemergemeinschaft", "weg "}},
	{"grundsteuer", []string{"grundsteuer", "grundbesitzabgaben"}},
	{"muell", []string{"abfall", "muell", "entsorgung"}},
	{"abwasser", []string{"abwasser", "kanal", "niederschlagswasser"}},
	{"wasser", []string{"wasser"}},
	{"schornsteinfeger", []string{"schornstein", "bezirksschornstein", "kaminkehrer", "kehrbezirk", "kehr "}},
	{"heizung", []string{"heizoel", "fernwaerme", "erdgas", "gas abschlag", "gasabschlag", "heizung", "waermeliefer", "ista", "techem", "brunata", "minol"}},
	{"strom_allgemein", []string{"allgemeinstrom", "strom"}},
	{"versicherung", []string{"gebaeudeversicherung", "wohngebaeude", "versicherung", "haftpflicht"}},
	{"hausmeister", []string{"hausmeister", "hauswart"}},
	{"gartenpflege", []string{"garten", "gruenpflege"}},
	{"reinigung", []string{"treppenhausreinigung", "gebaeudereinigung", "reinigung"}},
	{"strassenreinigung", []string{"strassenreinigung", "winterdienst"}},
	{"kabel", []string{"kabel", "vodafone kabel", "antenne"}},
	{"kredit", []string{"darlehen", "leistungen per", "leistungen zum", "tilgung", "baufinanzierung", "annuitaet"}},
	{"verwaltung", []string{"hausverwaltung", "verwalter", "kontofuehrung", "entgelt"}},
	{"reparatur", []string{"handwerk", "reparatur", "sanitaer", "elektro", "maler", "dachdecker", "heizungsbau", "installation", "baumarkt", "obi", "hornbach", "bauhaus"}},
}

// contains reports whether w occurs at a word start in text; with a trailing space, as a whole word.
func contains(text string, w string) bool {
	whole := strings.HasSuffix(w, " ")
	w = strings.TrimSpace(classify.NormName(w))
	if w == "" {
		return false
	}
	if whole {
		return strings.Contains(" "+text+" ", " "+w+" ")
	}
	return strings.Contains(" "+text+" ", " "+w)
}

// GuessCostType guesses the cost type of an expense on the rent account.
func GuessCostType(counterparty, remittance string) string {
	text := classify.NormName(counterparty + " " + remittance)
	for _, k := range costKeywords {
		for _, w := range k.words {
			if contains(text, w) {
				return k.slug
			}
		}
	}
	return "sonstiges"
}

// ---------- Assignment ----------

// AssignInput bundles everything the assignment needs.
type AssignInput struct {
	Txns       []store.HVTxn
	Leases     []store.Lease
	Properties []store.Property
	Rules      map[string]store.HVRule
	OwnIBANs   map[string]bool
}

// Assign maps rent account transactions to leases, properties and cost types.
// Manually set assignments are left untouched. Only changes are returned.
func Assign(in AssignInput) []store.HVAssign {
	var single *int64
	if len(in.Properties) == 1 {
		id := in.Properties[0].ID
		single = &id
	}
	type cand struct {
		lease store.Lease
		iban  string
		names []string
	}
	var cands []cand
	for _, l := range in.Leases {
		c := cand{lease: l, iban: strings.ToUpper(strings.ReplaceAll(l.Tenant.IBAN, " ", ""))}
		for _, n := range []string{l.Tenant.Name, l.MatchText} {
			if n = strings.TrimSpace(classify.NormName(n)); len(n) >= 3 {
				c.names = append(c.names, n)
			}
		}
		// also the surname alone ("Max Mustermann" -> "mustermann")
		if parts := strings.Fields(classify.NormName(l.Tenant.Name)); len(parts) >= 2 && len(parts[len(parts)-1]) >= 4 {
			c.names = append(c.names, parts[len(parts)-1])
		}
		cands = append(cands, c)
	}
	// detect properties by name or address in the text ("Talstraße 3" = "Talstr. 3")
	type pcand struct {
		id    int64
		names []string
	}
	var pcands []pcand
	for _, p := range in.Properties {
		pc := pcand{id: p.ID}
		for _, n := range []string{p.Name, strings.SplitN(p.Address, ",", 2)[0]} {
			if n = streetNorm(n); len(n) >= 5 {
				pc.names = append(pc.names, n)
			}
		}
		pcands = append(pcands, pc)
	}
	propertyIn := func(text string) *int64 {
		t := streetNorm(text)
		for _, pc := range pcands {
			for _, n := range pc.names {
				if strings.Contains(" "+t+" ", " "+n+" ") {
					id := pc.id
					return &id
				}
			}
		}
		return single
	}
	activeOn := func(l store.Lease, d string) bool {
		return (l.Start == "" || d >= addDays(l.Start, -45)) && (l.End == "" || d <= addDays(l.End, 60))
	}

	var out []store.HVAssign
	for _, t := range in.Txns {
		if t.Source == "manual" {
			continue
		}
		a := store.HVAssign{ID: t.ID, Source: "auto"}
		iban := strings.ToUpper(strings.ReplaceAll(t.CounterpartyIBAN, " ", ""))
		switch {
		case iban != "" && in.OwnIBANs[iban]:
			a.CostType = "entnahme"
		case t.AmountCents > 0:
			text := classify.NormName(t.Counterparty + " " + t.Remittance)
			var best *store.Lease
			for i := range cands {
				c := &cands[i]
				if !activeOn(c.lease, t.Date) {
					continue
				}
				hit := c.iban != "" && c.iban == iban
				for _, n := range c.names {
					if !hit && contains(text, n) {
						hit = true
					}
				}
				if hit {
					best = &c.lease
					break
				}
			}
			if best != nil {
				id, pid := best.ID, best.PropertyID
				a.LeaseID, a.PropertyID, a.CostType = &id, &pid, "miete"
				if strings.Contains(text, "kaution") {
					a.CostType = "kaution"
				} else if strings.Contains(text, "nachzahlung") || strings.Contains(text, "nebenkostenabrechnung") {
					a.CostType = "nk_nachzahlung"
				}
			} else {
				a.CostType, a.PropertyID = "einnahme_sonst", propertyIn(t.Counterparty+" "+t.Remittance)
			}
		default:
			a.CostType, a.PropertyID = GuessCostType(t.Counterparty, t.Remittance), propertyIn(t.Counterparty+" "+t.Remittance)
			if r, ok := in.Rules[t.MerchantKey]; ok && t.MerchantKey != "" {
				if r.CostType != "" {
					a.CostType = r.CostType
				}
				if r.PropertyID != nil {
					a.PropertyID = r.PropertyID
				}
			}
		}
		if eqID(a.PropertyID, t.PropertyID) && eqID(a.LeaseID, t.LeaseID) && a.CostType == t.CostType && t.Source == "auto" {
			continue
		}
		out = append(out, a)
	}
	return out
}

func eqID(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func addDays(d string, n int) string {
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		return d
	}
	return t.AddDate(0, 0, n).Format("2006-01-02")
}

func day(s string) time.Time { t, _ := time.Parse("2006-01-02", s); return t }

func monthStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// RentAt returns the cold rent and utility prepayment (NK-Vorauszahlung) in effect on day d.
func RentAt(l store.Lease, d time.Time) (int64, int64) {
	cold, nk := l.RentCold, l.NKPrepay
	steps := append([]store.RentStep(nil), l.Steps...)
	sort.Slice(steps, func(i, j int) bool { return steps[i].ValidFrom < steps[j].ValidFrom })
	for _, s := range steps {
		if !day(s.ValidFrom).After(d) {
			cold, nk = s.RentCold, s.NKPrepay
		}
	}
	return cold, nk
}

// occupiedDays counts the days in [from, to) during which the lease is active.
func occupiedDays(l store.Lease, from, to time.Time) int {
	s, e := from, to
	if l.Start != "" && day(l.Start).After(s) {
		s = day(l.Start)
	}
	if l.End != "" && day(l.End).AddDate(0, 0, 1).Before(e) {
		e = day(l.End).AddDate(0, 0, 1)
	}
	if !e.After(s) {
		return 0
	}
	return int(e.Sub(s).Hours() / 24)
}

// streetNorm normalizes street names: "Talstraße 3" and "Talstr. 3" -> "talstr 3".
func streetNorm(s string) string {
	n := classify.NormName(s)
	n = strings.ReplaceAll(n, "strasse", "str")
	return strings.Join(strings.Fields(n), " ")
}
