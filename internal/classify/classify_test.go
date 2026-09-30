package classify

import (
	"testing"
	"time"
)

func TestMerchant(t *testing.T) {
	cases := []struct{ cp, rem, wantKey, wantDisplay string }{
		{"REWE Markt GmbH//Beispielstadt/DE", "2026-09-12T10:21 Debitk.12 2029-12", "rewe", "REWE Markt GmbH"},
		{"PayPal Europe S.a.r.l. et Cie S.C.A", "1041234567890 PP.8812.PP . NETFLIX INTERNATIONAL B.V., Ihr Einkauf bei NETFLIX INTERNATIONAL B.V.", "netflix", "Netflix International B.V."},
		{"PayPal (Europe) S.a r.l. et Cie", "Ihr Einkauf bei Zalando SE, Artikel 12345", "zalando", "Zalando SE"},
		{"Stadtwerke Beispielstadt AG", "Abschlag 09/2026 Kd-Nr 4711", "stadtwerke beispielstadt", "Stadtwerke Beispielstadt AG"},
		{"Abrechnung Karte", "Eigenbetriebe Abfallwi//Musterstadt/DE 19-09-2026T12:33:17 Kartennr. 5", "eigenbetriebe abfallwi", "Eigenbetriebe Abfallwi"},
		{"", "EDEKA Center 1234//Musterstadt/DE", "edeka center", "EDEKA Center 1234"},
		{"", "mandatereference:,creditorid:,remittanceinformation: Zins/Dividende ISIN IE00BD8KRH84 ISHSVII-C", "zins dividende isin", "Zins/Dividende ISIN IE00BD8KRH84 ISHSVII"},
		{"", "EREF+NOTPROVIDED SVWZ+Rewe Markt Musterstadt", "rewe musterstadt", "Rewe Markt Musterstadt"},
	}
	for _, c := range cases {
		d, k := Merchant(c.cp, c.rem)
		if k != c.wantKey {
			t.Errorf("Merchant(%q) key = %q, want %q", c.cp, k, c.wantKey)
		}
		if d != c.wantDisplay {
			t.Errorf("Merchant(%q) display = %q, want %q", c.cp, d, c.wantDisplay)
		}
	}
}

func TestClassify(t *testing.T) {
	ctx := Context{
		OwnIBANs: map[string]bool{"DE02120300000000202051": true},
		Rules:    []Rule{{Field: "counterparty", Pattern: "hausverwaltung schmidt", Slug: "wohnen"}},
		OwnNames: OwnNamesFrom([]string{"Erika Beispiel", "Girokonto", "Giro Komfort Plus"}),
	}
	cases := []struct {
		name string
		amt  int64
		cp   string
		iban string
		rem  string
		code string
		want string
	}{
		{"Supermarket card", -4380, "REWE Markt GmbH//Beispielstadt/DE", "", "Kartenzahlung", "", "lebensmittel"},
		{"Netflix via PayPal", -1399, "PayPal Europe S.a.r.l. et Cie S.C.A", "", "PP.8812.PP . NETFLIX INTERNATIONAL B.V., Ihr Einkauf bei NETFLIX INTERNATIONAL B.V.", "", "abos"},
		{"Zalando via PayPal", -8990, "PayPal Europe", "", "Ihr Einkauf bei Zalando SE", "", "kleidung"},
		{"Salary", 341255, "Musterfirma GmbH", "DE89370400440532013000", "LOHN/GEHALT 09/2026", "", "gehalt"},
		{"Child benefit", 51000, "Bundesagentur fuer Arbeit - Familienkasse", "", "KG123456 Kindergeld 09/26", "", "kindergeld"},
		{"Internal transfer", -150000, "Erika Beispiel", "DE02 1203 0000 0000 2020 51", "Haushaltsgeld", "", "umbuchung"},
		{"User rule", -145000, "Hausverwaltung Schmidt", "", "Wohnung 3", "", "wohnen"},
		{"Rent keyword", -145000, "Familie Weber", "", "Miete Oktober Wohnung EG", "", "wohnen"},
		{"Broadcasting fee", -5508, "Rundfunk ARD, ZDF, DRadio", "", "Beitragsnummer 123", "", "rundfunk"},
		{"Mobile not hardware store", -2999, "Telefonica Germany GmbH", "", "o2 Mobilfunk Rechnung", "", "mobilfunk"},
		{"OBI hardware store", -6210, "OBI Markt Musterstadt//Musterstadt/DE", "", "", "", "haus-garten"},
		{"Amazon Prime", -8990, "AMAZON EU S.A R.L., NIEDERLASSUNG DEUTSCHLAND", "", "Prime Mitgliedschaft 123-4567", "", "abos"},
		{"Amazon purchase", -2399, "AMAZON EU S.A R.L., NIEDERLASSUNG DEUTSCHLAND", "", "302-1234567-1234567 Amazon.de", "", "online-shopping"},
		{"Car loan", -28900, "Santander Consumer Bank AG", "", "Rate 12/48", "", "kredite"},
		{"Savings plan", -25000, "Trade Republic Bank GmbH", "", "Sparplan ETF", "", "sparen"},
		{"ATM", -10000, "", "", "GA NR00001234 BLZ51050015 0", "CWDL", "bargeld"},
		{"Merchant refund", 2399, "AMAZON EU S.A R.L.", "", "Erstattung 302-1234567", "", "erstattung"},
		{"Card with placeholder payee", -3450, "Abrechnung Karte", "", "Eigenbetriebe Abfallwi//Musterstadt/DE 19-09-2026T12:33:17 Kartennr. 5123", "", "haus-garten"},
		{"Card supermarket placeholder", -2210, "Abrechnung Karte", "", "REWE Markt GmbH//Nachbarstadt/DE 20-09-2026T10:01:00 Kartennr. 5123", "", "lebensmittel"},
		{"Own name, other account", -50000, "ERIKA BEISPIEL", "DE99100100100123456789", "Sparen Tagesgeld", "", "sparen"},
		{"Tesla financing via Openbank", -39200, "Openbank Deutschland AG", "", "Rate Fahrzeugfinanzierung", "", "kredite"},
		{"Vattenfall electricity", -11900, "Vattenfall Europe Sales", "", "S/836643590802/603314041661 Talstr.3 Strom Abschlag", "", "strom"},
		{"Vattenfall Gas", -21300, "Vattenfall Europe Sales", "", "S/836643344699/604814218113 Talstr.3 Gas Abschlag", "", "gas"},
		{"Mortgage", -73279, "Deutsche Bank AG", "", "Baufinanzierung 111 0292649 87, Leistungen zum 15.09.2026", "", "wohnen"},
		{"Installment loan stays loan", -28900, "Santander Consumer Bank AG", "", "Rate Autofinanzierung", "", "kredite"},
		{"Commerzbank loan installment", -52417, "Commerzbank AG", "", "LEISTUNGEN PER 31.08.2026, IBAN DE02500400000122249601, AZ 8312345 Darlehen", "", "wohnen"},
		{"Unknown", -1234, "Max Mustermann", "", "Danke fuers Leihen", "", "sonstiges"},
		{"Dividend without payee", 57, "", "", "mandatereference:,creditorid:,remittanceinformation: Zins/Dividende ISIN IE00BD8KRH84 ISHSVII-C", "", "kapitalertraege"},
		{"Interest from broker is no refund", 1234, "Trade Republic Bank GmbH", "", "Zinsgutschrift September", "", "kapitalertraege"},
		{"Insurance", -8733, "HUK-COBURG Allgemeine Versicherung AG", "", "Beitrag KFZ", "", "versicherung"},
	}
	for _, c := range cases {
		tx := Txn{AmountCents: c.amt, Counterparty: c.cp, CounterpartyIBAN: c.iban, Remittance: c.rem, BankCode: c.code}
		Classify(&tx, ctx)
		if tx.Slug != c.want {
			t.Errorf("%s: slug = %q (%s), want %q", c.name, tx.Slug, tx.Reason, c.want)
		}
	}
}

func TestCrossBookTransfer(t *testing.T) {
	ctx := Context{OwnIBANs: map[string]bool{"DE11": true, "DE22": true}, IBANBook: map[string]string{"DE11": "haushalt", "DE22": "verwaltung"}}
	in := Txn{AmountCents: 50000, Counterparty: "Max", CounterpartyIBAN: "DE22", Book: "haushalt"}
	Classify(&in, ctx)
	out := Txn{AmountCents: -80000, Counterparty: "Mietkonto", CounterpartyIBAN: "DE22", Book: "haushalt"}
	Classify(&out, ctx)
	same := Txn{AmountCents: -10000, Counterparty: "Max", CounterpartyIBAN: "DE11", Book: "haushalt"}
	Classify(&same, ctx)
	if in.Slug != "vermietung" || out.Slug != "zuschuss-vermietung" || same.Slug != "umbuchung" {
		t.Fatalf("%s %s %s", in.Slug, out.Slug, same.Slug)
	}
}

func TestManualIsKept(t *testing.T) {
	tx := Txn{AmountCents: -4380, Counterparty: "REWE", Slug: "geschenke", Source: "manual"}
	Classify(&tx, Context{})
	if tx.Slug != "geschenke" || tx.MerchantKey != "rewe" {
		t.Fatalf("manual category overwritten: %+v", tx)
	}
}

func d(s string) time.Time { t, _ := time.Parse("2006-01-02", s); return t }

func TestDetectRecurring(t *testing.T) {
	today := d("2026-09-27")
	var pts []Point
	id := int64(0)
	add := func(date string, amt int64, key, label, slug, group string) {
		id++
		pts = append(pts, Point{TxnID: id, Date: d(date), AmountCents: amt, MerchantKey: key, Merchant: label, Slug: slug, Group: group})
	}
	// Monthly subscription from unknown provider
	for _, m := range []string{"2026-04-03", "2026-05-03", "2026-06-04", "2026-07-03", "2026-08-03", "2026-09-03"} {
		add(m, -999, "cloudfoo", "CloudFoo", "sonstiges", "expenses")
	}
	// Salary with slightly varying amount
	for i, m := range []string{"2026-05-29", "2026-06-30", "2026-07-30", "2026-08-28", "2026-09-29"} {
		add(m, 341255+int64(i*1000), "musterfirma", "Musterfirma", "einnahmen-sonst", "income")
	}
	// Supermarket: irregular and varying -> no series
	for i, m := range []string{"2026-08-01", "2026-08-06", "2026-08-09", "2026-08-15", "2026-08-22", "2026-08-24", "2026-09-02"} {
		add(m, -int64(2000+i*1733), "rewe", "Rewe", "lebensmittel", "expenses")
	}
	// Amazon: yearly Prime among regular purchases
	add("2025-03-14", -8990, "amazon", "Amazon", "online-shopping", "expenses")
	add("2026-03-14", -8990, "amazon", "Amazon", "online-shopping", "expenses")
	for _, m := range []string{"2025-11-02", "2026-01-20", "2026-05-11", "2026-08-30"} {
		add(m, -2399-int64(len(m)*37), "amazon", "Amazon", "online-shopping", "expenses")
	}
	// Quarterly broadcasting fee
	for _, m := range []string{"2025-10-15", "2026-01-15", "2026-04-15", "2026-07-15"} {
		add(m, -5508, "rundfunk ard zdf", "Rundfunk", "rundfunk", "bills")
	}
	// Ended subscription (last payment in May)
	for _, m := range []string{"2026-02-10", "2026-03-10", "2026-04-10", "2026-05-10"} {
		add(m, -1299, "altabo", "AltAbo", "sonstiges", "expenses")
	}

	series := DetectRecurring(pts, today)
	got := map[string]Series{}
	for _, s := range series {
		got[s.Key] = s
	}
	check := func(key string, cycleDays int, kind string, ended bool) {
		t.Helper()
		s, ok := got[key]
		if !ok {
			t.Errorf("series %q not detected; detected: %v", key, keys(got))
			return
		}
		if s.CycleDays != cycleDays || s.Kind != kind || s.Ended != ended {
			t.Errorf("series %q: cycle=%d kind=%s ended=%v, want %d %s %v", key, s.CycleDays, s.Kind, s.Ended, cycleDays, kind, ended)
		}
	}
	check("cloudfoo", 30, "abo", false)
	check("musterfirma", 30, "einkommen", false)
	check("amazon#8990", 365, "abo", false)
	check("rundfunk ard zdf", 91, "fixkosten", false)
	check("altabo", 30, "abo", true)
	if _, ok := got["rewe"]; ok {
		t.Errorf("supermarket wrongly detected as series")
	}

	slugOf, srcOf := map[int64]string{}, map[int64]string{}
	for _, p := range pts {
		slugOf[p.TxnID], srcOf[p.TxnID] = p.Slug, "auto"
	}
	ref := Refine(series, slugOf, srcOf)
	counts := map[string]int{}
	for _, r := range ref {
		counts[r.Slug]++
	}
	if counts["gehalt"] != 5 {
		t.Errorf("salary refinement: %d, want 5", counts["gehalt"])
	}
	if counts["abos"] != 6+4+2 {
		t.Errorf("subscription refinement: %d, want 12", counts["abos"])
	}
	if m := got["cloudfoo"].MonthlyCents(); m != 999 {
		t.Errorf("monthly value cloudfoo = %d", m)
	}
	if m := got["amazon#8990"].MonthlyCents(); m != 749 {
		t.Errorf("monthly value Prime = %d, want 749", m)
	}
}

func keys(m map[string]Series) []string {
	var k []string
	for x := range m {
		k = append(k, x)
	}
	return k
}

func TestCleanRemittance(t *testing.T) {
	cases := []struct{ in, want string }{
		// ING: filled SEPA fields
		{"mandatereference:E2026202101645044,creditorid:DE19ZZZ00000437097,remittanceinformation:01M2M65SN8M1KE9PYRSQ4XQMDD", "01M2M65SN8M1KE9PYRSQ4XQMDD"},
		// empty fields, commas in the text itself stay
		{"mandatereference:,creditorid:,remittanceinformation:Miete Oktober, Wohnung 3", "Miete Oktober, Wohnung 3"},
		// other order and spacing
		{"remittanceinformation:Rechnung 123,mandatereference:ABC,creditorid:DE12", "Rechnung 123"},
		{"Mandatereference: X1 , Creditorid: DE99 , Remittanceinformation: Lastschrift Netflix", "Lastschrift Netflix"},
		// classic SEPA tags
		{"EREF+NOTPROVIDED MREF+M123 CRED+DE98ZZZ09999999999 SVWZ+Beitrag Oktober", "Beitrag Oktober"},
		// plain text is left alone
		{"Miete Oktober Wohnung EG", "Miete Oktober Wohnung EG"},
	}
	for _, c := range cases {
		if got := CleanRemittance(c.in); got != c.want {
			t.Errorf("CleanRemittance(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
