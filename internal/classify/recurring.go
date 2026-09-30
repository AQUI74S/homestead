package classify

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/AQUI74S/homestead/internal/domain"
)

// Point is a transaction as needed by recurring detection.
type Point struct {
	TxnID       int64
	Date        time.Time
	AmountCents int64 // signed
	MerchantKey string
	Merchant    string
	Slug        string // category after Classify
	Group       domain.Group
	AccountID   int64
}

// Series is a detected recurring payment.
type Series struct {
	Key         string // merchant_key, with "#<cent>" for amount clusters
	Direction   domain.Direction
	Label       string
	CycleDays   domain.Cycle
	MedianCents int64 // amount per payment (positive)
	LastCents   int64
	First, Last time.Time
	Next        time.Time
	Count       int
	Ended       bool
	Kind        domain.Kind
	Slug        string // category of the most recent payment
	AccountID   int64  // account of the most recent payment
	TxnIDs      []int64
}

// MonthlyCents converts the amount to a monthly value.
func (s Series) MonthlyCents() int64 { return s.CycleDays.MonthlyCents(s.MedianCents) }

// cycleRule says how a cycle is recognized: gaps may deviate by tol days, and at
// least minCount payments are needed.
type cycleRule struct {
	days          domain.Cycle
	tol, minCount int
}

var cycleRules = []cycleRule{
	{domain.Weekly, 2, 4}, {domain.Biweekly, 3, 3}, {domain.Monthly, 6, 3}, {domain.Bimonthly, 8, 3},
	{domain.Quarterly, 14, 3}, {domain.HalfYearly, 21, 2}, {domain.Yearly, 35, 2},
}

// Thresholds of the detection.
const (
	minExactShare     = 0.6   // share of gaps that must match the cycle ...
	minRegularShare   = 0.8   // ... or match it with one skipped payment
	amountTolShare    = 0.2   // amounts may deviate ±20 % from the median ...
	amountTolMinCents = 300   // ... or at least ±3 €
	minStableShare    = 0.7   // share of amounts within that tolerance
	fixedAmountShare  = 0.03  // "same amount" for subscriptions: ±3 % ...
	fixedAmountMin    = 50    // ... or at least ±0.50 €
	maxAboCents       = 15000 // unknown subscriptions cost at most 150 €
	minSalaryCents    = 30000 // regular monthly income from 300 € counts as salary
)

// DetectRecurring finds recurring payments. Each merchant is checked as a whole
// first; if that fails, individual amount clusters are checked (e.g. Amazon Prime among
// regular Amazon purchases).
func DetectRecurring(points []Point, today time.Time) []Series {
	groups := map[string][]Point{}
	for _, p := range points {
		if p.MerchantKey == "" || p.Group == domain.GroupTransfer || p.AmountCents == 0 {
			continue
		}
		gk := string(domain.DirectionOf(p.AmountCents)) + "|" + p.MerchantKey
		groups[gk] = append(groups[gk], p)
	}
	var out []Series
	for gk, pts := range groups {
		d, key, _ := strings.Cut(gk, "|")
		dir := domain.Direction(d)
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

func detect(pts []Point, key string, dir domain.Direction, today time.Time) (Series, bool) {
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
	var cyc *cycleRule
	for i := range cycleRules {
		if math.Abs(med-float64(cycleRules[i].days)) <= float64(cycleRules[i].tol) {
			cyc = &cycleRules[i]
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
	if float64(exact) < minExactShare*float64(len(gaps)) || float64(exact+skipped) < minRegularShare*float64(len(gaps)) {
		return Series{}, false
	}
	// amount stability around the median
	amts := make([]float64, len(uniq))
	for i, p := range uniq {
		amts[i] = float64(abs(p.AmountCents))
	}
	mA := median(amts)
	tolA := math.Max(amountTolShare*mA, amountTolMinCents)
	stable := 0
	for _, a := range amts {
		if math.Abs(a-mA) <= tolA {
			stable++
		}
	}
	if float64(stable) < minStableShare*float64(len(amts)) {
		return Series{}, false
	}
	last := uniq[len(uniq)-1]
	s := Series{
		Key: key, Direction: dir, Label: last.Merchant, CycleDays: cyc.days,
		MedianCents: int64(math.Round(mA)), LastCents: abs(last.AmountCents),
		First: uniq[0].Date, Last: last.Date, Count: len(uniq), Slug: last.Slug, AccountID: last.AccountID,
	}
	s.Next = cyc.days.Step(s.Last, 1)
	s.Ended = today.After(s.Next.AddDate(0, 0, 2*cyc.tol))
	for _, p := range pts {
		s.TxnIDs = append(s.TxnIDs, p.TxnID)
	}
	s.Kind = kindFor(s, last.Group, amts, mA)
	return s, true
}

// kindFor derives the kind of recurring payment from category and amount history.
func kindFor(s Series, group domain.Group, amts []float64, med float64) domain.Kind {
	if s.Direction == domain.DirectionIn {
		return domain.KindIncome
	}
	switch s.Slug {
	case domain.SlugSubscriptions, domain.SlugMemberships:
		return domain.KindSubscription
	case domain.SlugLoans, domain.SlugCreditCard:
		return domain.KindLoan
	case domain.SlugSavings:
		return domain.KindSavings
	}
	switch group {
	case domain.GroupBills:
		return domain.KindFixedCost
	case domain.GroupDebts:
		return domain.KindLoan
	case domain.GroupSavings:
		return domain.KindSavings
	}
	if IsAboMerchant(s.Label) {
		return domain.KindSubscription
	}
	// Unknown merchant, fixed small amount at a fixed interval: very likely a subscription
	if aboCandidateSlugs[s.Slug] {
		exact := true
		for _, a := range amts {
			if math.Abs(a-med) > math.Max(fixedAmountShare*med, fixedAmountMin) {
				exact = false
				break
			}
		}
		if exact && med <= maxAboCents && s.CycleDays >= domain.Monthly {
			return domain.KindSubscription
		}
	}
	return domain.KindOther
}

// aboCandidateSlugs are categories in which an unknown fixed recurring amount is
// most likely a subscription.
var aboCandidateSlugs = map[string]bool{domain.SlugOther: true, domain.SlugOnlineShopping: true, domain.SlugLeisure: true}

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
func Refine(series []Series, slugOf map[int64]string, sourceOf map[int64]domain.Source) []Refinement {
	var out []Refinement
	for _, s := range series {
		var slug, reason string
		switch {
		case s.Direction == domain.DirectionIn && s.CycleDays == domain.Monthly && s.MedianCents >= minSalaryCents:
			slug, reason = domain.SlugSalary, "Regelmäßiger monatlicher Eingang"
		case s.Direction == domain.DirectionOut && s.Kind == domain.KindSubscription:
			slug, reason = domain.SlugSubscriptions, "Wiederkehrend ("+s.CycleDays.Label()+", gleicher Betrag)"
		default:
			continue
		}
		for _, id := range s.TxnIDs {
			if sourceOf[id] != domain.SourceAuto {
				continue
			}
			cur := slugOf[id]
			if cur == domain.SlugOtherIncome && slug == domain.SlugSalary || aboCandidateSlugs[cur] && slug == domain.SlugSubscriptions {
				out = append(out, Refinement{TxnID: id, Slug: slug, Reason: reason})
			}
		}
	}
	return out
}
