package syncer

import (
	"context"

	"github.com/AQUI74S/homestead/internal/classify"
	"github.com/AQUI74S/homestead/internal/domain"
	"github.com/AQUI74S/homestead/internal/hv"
	"github.com/AQUI74S/homestead/internal/store"
)

// recurringHistoryMonths: series are detected in the last two-plus years, so that
// annual payments have two occurrences.
const recurringHistoryMonths = 26

// Reclassify reclassifies all transactions (except manually set ones), updates the
// detected recurring payments and re-assigns the rent account transactions.
func (s *Syncer) Reclassify(ctx context.Context) error {
	rows, err := s.st.ClassRows(ctx)
	if err != nil {
		return err
	}
	cctx, bookOf, err := s.classifyContext(ctx)
	if err != nil {
		return err
	}
	cats, err := s.st.Categories(ctx)
	if err != nil {
		return err
	}
	groupOf := map[string]domain.Group{}
	for _, c := range cats {
		groupOf[c.Slug] = c.Group
	}

	txns := make([]classify.Txn, len(rows))
	for i, r := range rows {
		t := classify.Txn{ID: r.ID, AmountCents: r.AmountCents, Counterparty: r.Counterparty, CounterpartyIBAN: r.CounterpartyIBAN,
			Remittance: r.Remittance, BankCode: r.BankCode, Slug: r.Slug, Source: r.Source, Reason: r.Reason, Book: bookOf[r.AccountID]}
		classify.Classify(&t, cctx)
		if _, ok := groupOf[t.Slug]; !ok && t.Source != domain.SourceManual {
			t.Slug = domain.SlugOther // category no longer exists
		}
		txns[i] = t
	}

	series := s.detectSeries(rows, txns, groupOf)
	recOf, err := s.storeSeries(ctx, series, txns)
	if err != nil {
		return err
	}

	var changed []store.ClassUpdate
	for i, r := range rows {
		t := txns[i]
		var rid *int64
		if id, ok := recOf[t.ID]; ok {
			rid = &id
		}
		if t.Merchant == r.Merchant && t.MerchantKey == r.MerchantKey && t.Slug == r.Slug && t.Source == r.Source &&
			t.Reason == r.Reason && eqPtr(rid, r.RecurringID) {
			continue
		}
		changed = append(changed, store.ClassUpdate{ID: t.ID, Merchant: t.Merchant, MerchantKey: t.MerchantKey,
			Slug: t.Slug, Source: t.Source, Reason: t.Reason, RecurringID: rid})
	}
	s.log.Info("classification", "transactions", len(rows), "changed", len(changed), "series", len(series))
	if err := s.st.ApplyClassification(ctx, changed); err != nil {
		return err
	}
	return hv.Reassign(ctx, s.st)
}

// classifyContext collects own accounts, names and rules; bookOf maps account IDs to books.
func (s *Syncer) classifyContext(ctx context.Context) (classify.Context, map[int64]domain.Book, error) {
	cctx := classify.Context{IBANBook: map[string]domain.Book{}}
	bookOf := map[int64]domain.Book{}
	var err error
	if cctx.OwnIBANs, err = s.st.OwnIBANs(ctx); err != nil {
		return cctx, nil, err
	}
	rules, err := s.st.Rules(ctx)
	if err != nil {
		return cctx, nil, err
	}
	for _, r := range rules {
		cctx.Rules = append(cctx.Rules, classify.Rule{Field: r.Field, Pattern: r.Pattern, Slug: r.Slug})
	}
	classify.OrderRules(cctx.Rules)
	if accs, err := s.st.Accounts(ctx); err == nil {
		var names []string
		for _, a := range accs {
			names = append(names, a.Name, a.DisplayName)
			bookOf[a.ID] = a.Book
			if a.IBAN != "" {
				cctx.IBANBook[domain.NormIBAN(a.IBAN)] = a.Book
			}
		}
		cctx.OwnNames = classify.OwnNamesFrom(names)
	}
	if settings, err := s.st.Settings(ctx); err == nil {
		for _, n := range settings.OwnNames() {
			if n = classify.NormName(n); n != "" {
				cctx.OwnNames = append(cctx.OwnNames, n)
			}
		}
	}
	return cctx, bookOf, nil
}

// detectSeries finds recurring payments and refines the categories of their
// transactions in txns (e.g. regular income becomes salary).
func (s *Syncer) detectSeries(rows []store.ClassRow, txns []classify.Txn, groupOf map[string]domain.Group) []classify.Series {
	cutoff := s.now().AddDate(0, -recurringHistoryMonths, 0)
	var pts []classify.Point
	slugOf, srcOf := map[int64]string{}, map[int64]domain.Source{}
	for i, r := range rows {
		t := txns[i]
		slugOf[t.ID], srcOf[t.ID] = t.Slug, t.Source
		if r.BookingDate.Before(cutoff) {
			continue
		}
		pts = append(pts, classify.Point{TxnID: t.ID, Date: r.BookingDate, AmountCents: t.AmountCents, MerchantKey: t.MerchantKey,
			Merchant: t.Merchant, Slug: t.Slug, Group: groupOf[t.Slug], AccountID: r.AccountID})
	}
	series := classify.DetectRecurring(pts, s.now())
	idx := map[int64]int{}
	for i, t := range txns {
		idx[t.ID] = i
	}
	for _, rf := range classify.Refine(series, slugOf, srcOf) {
		if i, ok := idx[rf.TxnID]; ok {
			txns[i].Slug, txns[i].Reason = rf.Slug, rf.Reason
		}
	}
	return series
}

// storeSeries saves the series (with the category of their latest payment after
// refinement) and returns the series ID per transaction ID.
func (s *Syncer) storeSeries(ctx context.Context, series []classify.Series, txns []classify.Txn) (map[int64]int64, error) {
	idx := map[int64]int{}
	for i, t := range txns {
		idx[t.ID] = i
	}
	var ups []store.RecurringUpsert
	for _, sr := range series {
		if len(sr.TxnIDs) > 0 {
			sr.Slug = txns[idx[sr.TxnIDs[len(sr.TxnIDs)-1]]].Slug
		}
		ups = append(ups, store.RecurringUpsert{Key: sr.Key, Direction: sr.Direction, Label: sr.Label, Kind: sr.Kind, Slug: sr.Slug,
			CycleDays: sr.CycleDays, Count: sr.Count, AvgCents: sr.MedianCents, LastCents: sr.LastCents,
			First: sr.First, Last: sr.Last, Next: sr.Next, Ended: sr.Ended, AccountID: sr.AccountID})
	}
	ids, err := s.st.SyncRecurring(ctx, ups)
	if err != nil {
		return nil, err
	}
	recOf := map[int64]int64{}
	for _, sr := range series {
		id := ids[store.SeriesKey(sr.Direction, sr.Key)]
		for _, tid := range sr.TxnIDs {
			recOf[tid] = id
		}
	}
	return recOf, nil
}

func eqPtr(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
