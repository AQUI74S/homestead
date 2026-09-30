package classify

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Point is a transaction as needed by recurring detection.
type Point struct {
	TxnID       int64
	Date        time.Time
	AmountCents int64 // signed
	MerchantKey string
	Merchant    string
	Slug        string // category after Classify
	Group       string // income | bills | expenses | savings | debts | transfer
	AccountID   int64
}

// Series is a detected recurring payment.
type Series struct {
	Key         string // merchant_key, with "#<cent>" for amount clusters
	Direction   string // in | out
	Label       string
	CycleDays   int
	MedianCents int64 // amount per payment (positive)
	LastCents   int64
	First, Last time.Time
	Next        time.Time
	Count       int
	Ended       bool
	Kind        string // abo | fixkosten | einkommen | kredit | sparen | sonstiges
	Slug        string // category of the most recent payment
	AccountID   int64  // account of the most recent payment
	TxnIDs      []int64
}

// MonthlyCents converts the amount to a monthly value.
func (s Series) MonthlyCents() int64 {
	perYear := map[int]float64{7: 52, 14: 26, 30: 12, 61: 6, 91: 4, 182: 2, 365: 1}[s.CycleDays]
	if perYear == 0 {
		perYear = 365 / float64(s.CycleDays)
	}
	return int64(math.Round(float64(s.MedianCents) * perYear / 12))
}

type cycle struct{ days, tol, minCount int }

var cycles = []cycle{
	{7, 2, 4}, {14, 3, 3}, {30, 6, 3}, {61, 8, 3}, {91, 14, 3}, {182, 21, 2}, {365, 35, 2},
}

// CycleLabel returns a German label for the cycle length.
func CycleLabel(days int) string {
	switch days {
	case 7:
		return "Wöchentlich"
	case 14:
		return "Alle 2 Wochen"
	case 30:
		return "Monatlich"
	case 61:
		return "Alle 2 Monate"
	case 91:
		return "Vierteljährlich"
	case 182:
		return "Halbjährlich"
	case 365:
		return "Jährlich"
	}
	return fmt.Sprintf("Alle %d Tage", days)
}

// DetectRecurring finds recurring payments. Each merchant is checked as a whole
// first; if that fails, individual amount clusters are checked (e.g. Amazon Prime among
// regular Amazon purchases).
func DetectRecurring(points []Point, today time.Time) []Series {
	groups := map[string][]Point{}
	for _, p := range points {
		if p.MerchantKey == "" || p.Group == "transfer" || p.AmountCents == 0 {
			continue
		}
		dir := "out"
		if p.AmountCents > 0 {
			dir = "in"
		}
		groups[dir+"|"+p.MerchantKey] = append(groups[dir+"|"+p.MerchantKey], p)
	}
	var out []Series
	for gk, pts := range groups {
		dir, key, _ := strings.Cut(gk, "|")
		if s, ok := detect(pts, key, dir, today); ok {
			out = append(out, s)
			continue
		}
		// amount clusters (exactly the same amount)
		byAmt := map[int64][]Point{}
		for _, p := range pts {
			byAmt[abs(p.AmountCents)] = append(byAmt[abs(p.AmountCents)], p)
		}
		for amt, cp := range byAmt {
			if len(cp) < 2 || len(cp) == len(pts) {
				continue
			}
			if s, ok := detect(cp, fmt.Sprintf("%s#%d", key, amt), dir, today); ok {
				out = append(out, s)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MonthlyCents() > out[j].MonthlyCents() })
	return out
}

func detect(pts []Point, key, dir string, today time.Time) (Series, bool) {
	sort.Slice(pts, func(i, j int) bool { return pts[i].Date.Before(pts[j].Date) })
	// one payment per day
	var uniq []Point
	for _, p := range pts {
		if len(uniq) > 0 && sameDay(uniq[len(uniq)-1].Date, p.Date) {
			continue
		}
		uniq = append(uniq, p)
	}
	if len(uniq) < 2 {
		return Series{}, false
	}
	var gaps []float64
	for i := 1; i < len(uniq); i++ {
		gaps = append(gaps, uniq[i].Date.Sub(uniq[i-1].Date).Hours()/24)
	}
	med := median(gaps)
	var cyc *cycle
	for i := range cycles {
		if math.Abs(med-float64(cycles[i].days)) <= float64(cycles[i].tol) {
			cyc = &cycles[i]
			break
		}
	}
	if cyc == nil || len(uniq) < cyc.minCount {
		return Series{}, false
	}
	// regularity: gaps match the cycle (one skipped occurrence is allowed)
	exact, skipped := 0, 0
	for _, g := range gaps {
		switch {
		case math.Abs(g-float64(cyc.days)) <= float64(cyc.tol):
			exact++
		case math.Abs(g-2*float64(cyc.days)) <= float64(2*cyc.tol):
			skipped++
		}
	}
	if float64(exact) < 0.6*float64(len(gaps)) || float64(exact+skipped) < 0.8*float64(len(gaps)) {
		return Series{}, false
	}
	// amount stability: ±20 % or at least ±3 € around the median
	amts := make([]float64, len(uniq))
	for i, p := range uniq {
		amts[i] = float64(abs(p.AmountCents))
	}
	mA := median(amts)
	tolA := math.Max(0.2*mA, 300)
	stable := 0
	for _, a := range amts {
		if math.Abs(a-mA) <= tolA {
			stable++
		}
	}
	if float64(stable) < 0.7*float64(len(amts)) {
		return Series{}, false
	}
	last := uniq[len(uniq)-1]
	s := Series{
		Key: key, Direction: dir, Label: last.Merchant, CycleDays: cyc.days,
		MedianCents: int64(math.Round(mA)), LastCents: abs(last.AmountCents),
		First: uniq[0].Date, Last: last.Date, Count: len(uniq), Slug: last.Slug, AccountID: last.AccountID,
	}
	s.Next = s.Last.AddDate(0, 0, cyc.days)
	if cyc.days == 30 {
		s.Next = s.Last.AddDate(0, 1, 0)
	} else if cyc.days == 365 {
		s.Next = s.Last.AddDate(1, 0, 0)
	}
	s.Ended = today.After(s.Next.AddDate(0, 0, 2*cyc.tol))
	for _, p := range pts {
		s.TxnIDs = append(s.TxnIDs, p.TxnID)
	}
	s.Kind = kindFor(s, last.Group, amts, mA)
	return s, true
}

// kindFor derives the kind of recurring payment from category and amount history.
func kindFor(s Series, group string, amts []float64, med float64) string {
	if s.Direction == "in" {
		return "einkommen"
	}
	switch s.Slug {
	case "abos", "mitgliedschaft":
		return "abo"
	case "kredite", "kreditkarte":
		return "kredit"
	case "sparen":
		return "sparen"
	}
	switch group {
	case "bills":
		return "fixkosten"
	case "debts":
		return "kredit"
	case "savings":
		return "sparen"
	}
	if IsAboMerchant(s.Label) {
		return "abo"
	}
	// Unknown merchant, fixed small amount at a fixed interval: very likely a subscription
	if s.Slug == "sonstiges" || s.Slug == "online-shopping" || s.Slug == "freizeit" {
		exact := true
		for _, a := range amts {
			if math.Abs(a-med) > math.Max(0.03*med, 50) {
				exact = false
				break
			}
		}
		if exact && med <= 15000 && s.CycleDays >= 30 {
			return "abo"
		}
	}
	return "sonstiges"
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func median(xs []float64) float64 {
	c := append([]float64(nil), xs...)
	sort.Float64s(c)
	n := len(c)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return c[n/2]
	}
	return (c[n/2-1] + c[n/2]) / 2
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// Refinement is a category change resulting from a detected series.
type Refinement struct {
	TxnID  int64
	Slug   string
	Reason string
}

// Refine derives better categories for auto-classified transactions from series:
// regular larger incoming payments become salary, unknown subscriptions "Abos & Streaming".
func Refine(series []Series, slugOf map[int64]string, sourceOf map[int64]string) []Refinement {
	var out []Refinement
	for _, s := range series {
		var slug, reason string
		switch {
		case s.Direction == "in" && s.CycleDays == 30 && s.MedianCents >= 30000:
			slug, reason = "gehalt", "Regelmäßiger monatlicher Eingang"
		case s.Direction == "out" && s.Kind == "abo":
			slug, reason = "abos", "Wiederkehrend ("+CycleLabel(s.CycleDays)+", gleicher Betrag)"
		default:
			continue
		}
		for _, id := range s.TxnIDs {
			if sourceOf[id] != "auto" {
				continue
			}
			cur := slugOf[id]
			if cur == "einnahmen-sonst" && slug == "gehalt" ||
				(cur == "sonstiges" || cur == "online-shopping" || cur == "freizeit") && slug == "abos" {
				out = append(out, Refinement{TxnID: id, Slug: slug, Reason: reason})
			}
		}
	}
	return out
}
