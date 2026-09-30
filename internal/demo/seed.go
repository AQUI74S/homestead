// Package demo fills an empty installation in demo mode with sample data so that
// both areas (household budget and property management) show something right away.
package demo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/AQUI74S/homestead/internal/budget"
	"github.com/AQUI74S/homestead/internal/domain"
	"github.com/AQUI74S/homestead/internal/hv"
	"github.com/AQUI74S/homestead/internal/store"
	"github.com/AQUI74S/homestead/internal/syncer"
)

// Seed only runs if no bank connection exists yet. It connects the three demo banks
// via the same path as the UI, fetches transactions, creates budgets as well as
// properties, units and leases, and assigns everything.
func Seed(ctx context.Context, st *store.Store, sy *syncer.Syncer, log *slog.Logger) error {
	conns, err := st.Connections(ctx)
	if err != nil {
		return err
	}
	if len(conns) > 0 {
		return nil
	}
	log.Info("demo: empty database, creating sample data")

	for _, b := range []struct {
		bank string
		book domain.Book
	}{
		{"Demo-Sparkasse", domain.BookHousehold}, {"Demo-Direktbank", domain.BookHousehold}, {"Demo-Hausbank", domain.BookProperty},
	} {
		state := randomState()
		if _, err := st.CreatePendingConnection(ctx, b.bank, "DE", state, b.book); err != nil {
			return err
		}
		if _, err := sy.CompleteAuth(ctx, state, "demo:"+b.bank, nil); err != nil {
			return fmt.Errorf("%s verbinden: %w", b.bank, err)
		}
	}

	// assign accounts to persons (for the couple split)
	_ = st.SetSetting(ctx, domain.SettingNameA, "Alex")
	_ = st.SetSetting(ctx, domain.SettingNameB, "Kim")
	accts, err := st.Accounts(ctx)
	if err != nil {
		return err
	}
	for _, a := range accts {
		switch a.ProviderUID {
		case "demo-giro-a":
			err = st.UpdateAccount(ctx, a.ID, domain.OwnerA, "Girokonto Alex", true, a.Book)
		case "demo-giro-b":
			err = st.UpdateAccount(ctx, a.ID, domain.OwnerB, "Girokonto Kim", true, a.Book)
		case "demo-joint":
			err = st.UpdateAccount(ctx, a.ID, domain.OwnerJoint, "Gemeinschaftskonto", true, a.Book)
		}
		if err != nil {
			return err
		}
	}

	if err := seedHV(ctx, st); err != nil {
		return fmt.Errorf("Hausverwaltung: %w", err)
	}

	// CompleteAuth may already have started a background sync; wait for it, then do a full sync.
	for i := 0; i < 600 && sy.Status().Running; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if err := sy.SyncAll(ctx); err != nil {
		return err
	}

	periods, err := budget.LoadPeriods(ctx, st)
	if err != nil {
		return err
	}
	if _, _, err := budget.SuggestBudgets(ctx, st, periods, time.Now(), false); err != nil {
		return err
	}
	log.Info("demo: sample data created")
	return nil
}

func seedHV(ctx context.Context, st *store.Store) error {
	price := int64(45000000)
	tal, err := st.SaveProperty(ctx, store.Property{Name: "Talstraße 3", Address: "Talstraße 3, 12345 Musterstadt",
		PurchasePrice: &price, Notes: "Zweifamilienhaus, Baujahr 1978"})
	if err != nil {
		return err
	}
	priceETW := int64(21000000)
	etw, err := st.SaveProperty(ctx, store.Property{Name: "ETW Beispielstadt", Address: "Rheinstraße 12, 10115 Beispielstadt",
		PurchasePrice: &priceETW, Notes: "Eigentumswohnung, verwaltet von WEG Rheinstraße 12"})
	if err != nil {
		return err
	}
	eg, err := st.SaveUnit(ctx, store.Unit{PropertyID: tal, Name: "EG", AreaCents: 7200})
	if err != nil {
		return err
	}
	og, err := st.SaveUnit(ctx, store.Unit{PropertyID: tal, Name: "OG", AreaCents: 9400})
	if err != nil {
		return err
	}
	w4, err := st.SaveUnit(ctx, store.Unit{PropertyID: etw, Name: "Whg. 4", AreaCents: 6200})
	if err != nil {
		return err
	}

	now := time.Now()
	d := func(years, months int) string {
		t := domain.FirstOfMonth(now).AddDate(years, months, 0)
		return t.Format(domain.DateLayout)
	}
	// Anna Schmidt: tenant for years, last increase long ago → §558 BGB increase possible
	if _, err := st.SaveLease(ctx, store.Lease{UnitID: eg, Tenant: store.Tenant{Name: "Anna Schmidt", IBAN: "DE21500105175555111111",
		Email: "anna.schmidt@example.org"}, Start: d(-6, 0), RentCold: 80000, NKPrepay: 20000, DueDay: hv.DefaultDueDay, Deposit: 240000,
		DepositPaid: true, Persons: 1, RentType: hv.RentFixed, LastIncrease: d(-2, -3)}); err != nil {
		return err
	}
	// Yilmaz family: Staffelmiete (graduated rent), next step in a few months; one partial payment in the history
	yil, err := st.SaveLease(ctx, store.Lease{UnitID: og, Tenant: store.Tenant{Name: "Mehmet Yilmaz", IBAN: "DE44700202700666222222",
		Phone: "0612 3456789"}, Start: d(-3, 0), RentCold: 100000, NKPrepay: 25000, DueDay: hv.DefaultDueDay, Deposit: 300000,
		DepositPaid: true, Persons: 4, RentType: hv.RentStaged})
	if err != nil {
		return err
	}
	if _, err := st.AddRentStep(ctx, store.RentStep{LeaseID: yil, ValidFrom: d(0, 3), RentCold: 104000, NKPrepay: 25000}); err != nil {
		return err
	}
	// Lukas Weber: Indexmiete (index-linked rent), deposit still open, last rent missing
	if _, err := st.SaveLease(ctx, store.Lease{UnitID: w4, Tenant: store.Tenant{Name: "Lukas Weber", IBAN: "DE89370400440777333333"},
		Start: d(-1, -6), RentCold: 62000, NKPrepay: 16000, DueDay: hv.DefaultDueDay, Deposit: 186000, DepositPaid: false, Persons: 1,
		RentType: hv.RentIndex}); err != nil {
		return err
	}

	// costs without a bank booking (cash/other account) for the Nebenkostenabrechnung
	lastYear := now.Year() - 1
	for _, c := range []store.ManualCost{
		{PropertyID: tal, Year: lastYear, CostType: "gartenpflege", Amount: 36000, Note: "Gärtner, bar bezahlt"},
		{PropertyID: tal, Year: lastYear, CostType: "heizung", Amount: 248000, Note: "Heizöl-Lieferung (anderes Konto)"},
	} {
		if _, err := st.AddManualCost(ctx, c); err != nil {
			return err
		}
	}
	due := now.AddDate(0, 0, 10).Format(domain.DateLayout)
	if _, err := st.AddReminder(ctx, store.Reminder{PropertyID: &tal, DueDate: due, Title: "Rauchmelder-Wartung Talstraße 3 beauftragen"}); err != nil {
		return err
	}
	return nil
}

func randomState() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "demo-" + hex.EncodeToString(b)
}
