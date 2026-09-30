package enablebanking

import (
	"context"
	"fmt"
	"math/rand"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Demo is a simulated bank with three accounts and just over 15 months of realistic transactions.
// It goes through the same flow as the real API (choose bank, consent, session, fetch).
type Demo struct {
	now   func() time.Time
	cache map[string][]Transaction
}

func NewDemo() *Demo { return &Demo{now: time.Now, cache: map[string][]Transaction{}} }

const (
	demoIBANA     = "DE12500105170648489890"
	demoIBANJoint = "DE89370400440532013000"
	demoIBANB     = "DE75512108001245126199"
	demoIBANMiet  = "DE53510500150199887766"
)

var demoAccounts = map[string]Account{
	"demo-giro-a": {UID: "demo-giro-a", AccountID: AccountID{IBAN: demoIBANA}, Name: "Girokonto", Currency: "EUR", Product: "Giro Komfort"},
	"demo-joint":  {UID: "demo-joint", AccountID: AccountID{IBAN: demoIBANJoint}, Name: "Gemeinschaftskonto", Currency: "EUR", Product: "Giro Gemeinschaft"},
	"demo-giro-b": {UID: "demo-giro-b", AccountID: AccountID{IBAN: demoIBANB}, Name: "Girokonto", Currency: "EUR", Product: "Online-Giro"},
	"demo-miet":   {UID: "demo-miet", AccountID: AccountID{IBAN: demoIBANMiet}, Name: "Mietkonto", Currency: "EUR", Product: "Geschäftskonto"},
}

func (d *Demo) ASPSPs(ctx context.Context, country string) ([]ASPSP, error) {
	return []ASPSP{
		{Name: "Demo-Sparkasse", Country: "DE", BIC: "DEMODEF1XXX", MaximumConsentValidity: 180 * 86400, PSUTypes: []string{"personal"}},
		{Name: "Demo-Direktbank", Country: "DE", BIC: "DEMODEF2XXX", MaximumConsentValidity: 90 * 86400, PSUTypes: []string{"personal"}},
		{Name: "Demo-Hausbank", Country: "DE", BIC: "DEMODEF3XXX", MaximumConsentValidity: 180 * 86400, PSUTypes: []string{"personal"}},
	}, nil
}

func (d *Demo) StartAuth(ctx context.Context, r AuthRequest) (string, error) {
	// The real bank would show its login/TAN page here and then redirect back.
	q := url.Values{"code": {"demo:" + r.ASPSPName}, "state": {r.State}}
	return r.RedirectURL + "?" + q.Encode(), nil
}

func (d *Demo) CreateSession(ctx context.Context, code string) (*Session, error) {
	bank := strings.TrimPrefix(code, "demo:")
	s := &Session{SessionID: fmt.Sprintf("demo-session-%s-%d", strings.ToLower(bank), d.now().UnixNano())}
	s.ASPSP.Name, s.ASPSP.Country = bank, "DE"
	s.Access.ValidUntil = d.now().Add(90 * 24 * time.Hour)
	if bank == "Demo-Direktbank" {
		s.Accounts = []Account{demoAccounts["demo-giro-b"]}
	} else if bank == "Demo-Hausbank" {
		s.Accounts = []Account{demoAccounts["demo-miet"]}
	} else {
		s.Accounts = []Account{demoAccounts["demo-giro-a"], demoAccounts["demo-joint"]}
	}
	return s, nil
}

func (d *Demo) DeleteSession(ctx context.Context, id string) error { return nil }

func (d *Demo) Balances(ctx context.Context, uid string) ([]Balance, error) {
	txs := d.all(uid)
	start := map[string]int64{"demo-giro-a": 182340, "demo-joint": 415020, "demo-giro-b": 96310, "demo-miet": 850000}[uid]
	bal := start
	for _, t := range txs {
		c, _ := ParseCents(t.TransactionAmount.Amount)
		if t.CreditDebitIndicator == "DBIT" {
			c = -c
		}
		bal += c
	}
	return []Balance{{Name: "Buchungssaldo", BalanceType: "CLBD", BalanceAmount: Amount{Currency: "EUR", Amount: centsStr(bal)}, ReferenceDate: d.now().Format(apiDate)}}, nil
}

func (d *Demo) Transactions(ctx context.Context, uid string, from, to time.Time, cont string) (*TransactionPage, error) {
	var sel []Transaction
	for _, t := range d.all(uid) {
		bd, _ := time.Parse(apiDate, t.BookingDate)
		if (!from.IsZero() && bd.Before(from)) || (!to.IsZero() && bd.After(to)) {
			continue
		}
		sel = append(sel, t)
	}
	off, _ := strconv.Atoi(cont)
	const page = 100
	end := off + page
	if end > len(sel) {
		end = len(sel)
	}
	p := &TransactionPage{Transactions: sel[off:end]}
	if end < len(sel) {
		p.ContinuationKey = strconv.Itoa(end)
	}
	return p, nil
}

func (d *Demo) all(uid string) []Transaction {
	day := d.now().Format(apiDate)
	if t, ok := d.cache[uid+day]; ok {
		return t
	}
	t := generate(uid, d.now())
	d.cache[uid+day] = t
	return t
}

func centsStr(c int64) string {
	sign := ""
	if c < 0 {
		sign, c = "-", -c
	}
	return fmt.Sprintf("%s%d.%02d", sign, c/100, c%100)
}

type gen struct {
	uid string
	r   *rand.Rand
	out []Transaction
	n   int
}

func (g *gen) add(date time.Time, cents int64, party, iban, remit, code string) {
	g.n++
	ind := "DBIT"
	if cents > 0 {
		ind = "CRDT"
	} else {
		cents = -cents
	}
	t := Transaction{
		EntryReference:        fmt.Sprintf("%s-%s-%04d", g.uid, date.Format("20060102"), g.n),
		BookingDate:           date.Format(apiDate),
		ValueDate:             date.Format(apiDate),
		Status:                "BOOK",
		CreditDebitIndicator:  ind,
		TransactionAmount:     Amount{Currency: "EUR", Amount: centsStr(cents)},
		RemittanceInformation: []string{remit},
	}
	p := &Party{Name: party}
	var a *AccountID
	if iban != "" {
		a = &AccountID{IBAN: iban}
	}
	if ind == "DBIT" {
		t.Creditor, t.CreditorAccount = p, a
	} else {
		t.Debtor, t.DebtorAccount = p, a
	}
	if code != "" {
		t.BankTransactionCode = &struct {
			Code        string `json:"code"`
			SubCode     string `json:"sub_code"`
			Description string `json:"description"`
		}{Code: "PMNT", SubCode: code}
	}
	g.out = append(g.out, t)
}

// workday moves weekends to the next Monday (or the previous Friday if back=true).
func workday(t time.Time, back bool) time.Time {
	for t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
		if back {
			t = t.AddDate(0, 0, -1)
		} else {
			t = t.AddDate(0, 0, 1)
		}
	}
	return t
}

func (g *gen) between(lo, hi int64) int64 { return lo + g.r.Int63n(hi-lo+1) }

func generate(uid string, now time.Time) []Transaction {
	g := &gen{uid: uid, r: rand.New(rand.NewSource(int64(len(uid)) * 7919))}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	start := time.Date(today.Year()-1, today.Month()-3, 1, 0, 0, 0, 0, time.UTC)

	for m := start; !m.After(today); m = m.AddDate(0, 1, 0) {
		y, mo := m.Year(), m.Month()
		dt := func(day int) time.Time {
			last := time.Date(y, mo+1, 0, 0, 0, 0, 0, time.UTC).Day()
			if day > last {
				day = last
			}
			return time.Date(y, mo, day, 0, 0, 0, 0, time.UTC)
		}
		mm := m.Format("01/2006")
		lastDay := time.Date(y, mo+1, 0, 0, 0, 0, 0, time.UTC).Day()

		switch uid {
		case "demo-giro-a":
			g.add(workday(dt(lastDay-2), true), 341255+g.between(-800, 800), "Muster Software GmbH", "DE44500105175407324931", "LOHN/GEHALT "+mm, "")
			if mo == time.November {
				g.add(workday(dt(lastDay-2), true), 170600, "Muster Software GmbH", "DE44500105175407324931", "Weihnachtsgeld "+mm, "")
			}
			g.add(workday(dt(1), false), -160000, "Umbuchung Gemeinschaftskonto", demoIBANJoint, "Haushaltsgeld "+mm, "")
			g.add(dt(28), 50000, "Mietkonto", demoIBANMiet, "Entnahme Überschuss Mietkonto", "")
			g.add(dt(2), -25000, "Trade Republic Bank GmbH", "DE72100123450000012345", "Sparplan ETF "+mm, "")
			g.add(dt(5), -1399, "PayPal Europe S.a.r.l. et Cie S.C.A", "LU89751000135104200E", "1049"+strconv.Itoa(g.n)+" PP.8812.PP . NETFLIX INTERNATIONAL B.V., Ihr Einkauf bei NETFLIX INTERNATIONAL B.V.", "")
			g.add(dt(12), -1799, "Spotify AB", "", "Spotify P2A1B3C4 Premium Family", "")
			g.add(dt(8), -583, "Hetzner Online GmbH", "DE92760700120750007700", "Rechnung R00"+strconv.Itoa(1000+g.n), "")
			g.add(dt(3), -2300, "OpenAI LLC", "", "OPENAI *CHATGPT SUBSCR", "")
			g.add(dt(20), -2499, "Telefonica Germany GmbH + Co. OHG", "DE17700700100234567890", "o2 Rechnung Kd-Nr 6012345", "")
			g.add(workday(dt(1), false), -2499, "FitX Deutschland GmbH", "", "Mitgliedsbeitrag "+mm, "")
			g.add(dt(15), -28900, "Santander Consumer Bank AG", "DE50310108330000123456", "Rate Autofinanzierung Vertrag 887766", "")
			if mo == time.March {
				g.add(dt(14), -8990, "AMAZON EU S.A R.L., NIEDERLASSUNG DEUTSCHLAND", "", "Prime Mitgliedschaft Jahresgebuehr", "")
			}
			if mo == time.June {
				g.add(dt(3), -9400, "ADAC e.V.", "", "Mitgliedschaft ADAC Plus 2026", "")
			}
			for d := 3; d <= 28; d += 8 + g.r.Intn(4) {
				g.add(dt(d), -g.between(5200, 8900), "ARAL Station 1234//Musterstadt/DE", "", "Kartenzahlung girocard", "")
			}
			for i := 0; i < 2; i++ {
				g.add(dt(6+i*12+g.r.Intn(4)), -g.between(2290, 4590), "Lieferando.de", "", "Takeaway.com Bestellung", "")
				g.add(dt(4+i*13+g.r.Intn(5)), -g.between(1199, 8999), "AMAZON EU S.A R.L., NIEDERLASSUNG DEUTSCHLAND", "", fmt.Sprintf("30%d-%07d Amazon.de", g.r.Intn(9), g.r.Intn(9999999)), "")
			}
			if g.r.Intn(3) == 0 {
				g.add(dt(10+g.r.Intn(15)), -g.between(1990, 12990), "Decathlon//Beispielstadt/DE", "", "Kartenzahlung", "")
			}
			if g.r.Intn(4) == 0 {
				g.add(dt(10+g.r.Intn(15)), -g.between(2499, 6999), "Thalia Online", "", "Bestellung TH-"+strconv.Itoa(g.n), "")
			}
		case "demo-joint":
			g.add(workday(dt(1), false), 160000, "Umbuchung Girokonto", demoIBANA, "Haushaltsgeld "+mm, "")
			g.add(workday(dt(1), false), 90000, "Partnerin", demoIBANB, "Haushalt "+mm, "")
			g.add(dt(9), 51000, "Bundesagentur fuer Arbeit - Familienkasse", "DE63760000000076001608", "KG-Nr 123FK456789 Kindergeld "+mm, "")
			g.add(workday(dt(30), true), -125000, "Sparkasse Musterstadt", "DE28512500000000123456", "Darlehen 7001234 Rate Baufinanzierung", "")
			g.add(dt(15), -9500, "Stadtwerke Musterstadt", "DE43500500000001234567", "Abschlag Strom Vertragskonto 400123", "")
			g.add(dt(15), -11000, "Stadtwerke Musterstadt Erdgas", "DE21500500000098765432", "Abschlag Erdgas 400124", "")
			g.add(dt(22), -4495, "Telekom Deutschland GmbH", "DE11700100800001234567", "Festnetz/Internet Rechnung "+mm, "")
			g.add(dt(3), -18000, "Kita Sonnenschein e.V.", "DE66510500150123456789", "Betreuungsbeitrag Kind 1 "+mm, "")
			if mo%3 == 1 {
				g.add(dt(15), -5508, "Rundfunk ARD, ZDF, DRadio", "DE70700000000000005108", "Beitragsnummer 123456789 Rundfunkbeitrag", "")
				g.add(dt(10), -11800, "Wasserverband Musterstadt", "DE10512500000000556677", "Wassergeld Abschlag", "")
				g.add(dt(1), -3850, "Allianz Versicherungs-AG", "DE27700800000123456789", "Hausratversicherung HR-445566", "")
			}
			if mo == time.January {
				g.add(dt(2), -61200, "HUK-COBURG Allgemeine Versicherung AG", "DE36783500000001234567", "Kfz-Versicherung Beitrag 2026", "")
				g.add(dt(3), -7800, "Gothaer Allgemeine Versicherung AG", "", "Privathaftpflicht Jahresbeitrag", "")
			}
			shops := []string{"REWE Markt GmbH//Musterstadt/DE", "EDEKA Center Mueller//Musterstadt/DE", "LIDL DIENSTL. GMBH//Musterstadt/DE", "ALDI SUED//Nachbarstadt/DE"}
			for d := 1; d <= 28; d += 2 + g.r.Intn(3) {
				g.add(dt(d), -g.between(1800, 14500), shops[g.r.Intn(len(shops))], "", "Kartenzahlung girocard", "")
			}
			g.add(dt(7), -g.between(1500, 4800), "dm-drogerie markt//Musterstadt/DE", "", "Kartenzahlung", "")
			g.add(dt(21), -g.between(1500, 4800), "dm-drogerie markt//Musterstadt/DE", "", "Kartenzahlung", "")
			g.add(dt(12), -20000, "", "", "GA NR00001234 BLZ51050015 Bargeldauszahlung", "CWDL")
			if g.r.Intn(2) == 0 {
				g.add(dt(16+g.r.Intn(8)), -g.between(2500, 18900), "OBI Markt Musterstadt//Musterstadt/DE", "", "Kartenzahlung", "")
			}
			if g.r.Intn(3) == 0 {
				g.add(dt(5+g.r.Intn(20)), -g.between(890, 3490), "Hirsch-Apotheke//Nachbarstadt/DE", "", "Kartenzahlung", "")
			}
			if g.r.Intn(3) == 0 {
				g.add(dt(5+g.r.Intn(20)), -g.between(1999, 5999), "Smyths Toys//Beispielstadt/DE", "", "Kartenzahlung", "")
			}
			if mo == time.December {
				g.add(dt(18), -g.between(4000, 9000), "Douglas GmbH//Beispielstadt/DE", "", "Kartenzahlung", "")
				g.add(dt(20), -g.between(3000, 8000), "IKEA Deutschland GmbH//Beispielstadt/DE", "", "Kartenzahlung", "")
			}
		case "demo-miet":
			// Property Talstraße 3 (ground floor Anna Schmidt, upper floor Yilmaz family), condo Rheinstraße 12 (Lukas Weber)
			g.add(dt(2), 100000, "Anna Schmidt", "DE21500105175555111111", "Miete "+mm+" Talstr. 3 EG", "")
			yil := int64(125000)
			if m.Equal(time.Date(today.Year(), today.Month()-2, 1, 0, 0, 0, 0, time.UTC)) {
				yil = 60000 // partial payment
			}
			g.add(dt(4), yil, "Mehmet Yilmaz", "DE44700202700666222222", "Mietzahlung "+mm, "")
			if !m.Equal(time.Date(today.Year(), today.Month()-1, 1, 0, 0, 0, 0, time.UTC)) { // last month missing
				g.add(dt(3), 78000, "Lukas Weber", "DE89370400440777333333", "Miete Wohnung Rheinstr. 12", "")
			}
			g.add(dt(1), -110000, "Kreissparkasse Musterland", "DE15510500150100200300", "Darlehen 88776655 Talstr. 3 Leistungen per "+dt(1).Format("02.01.2006"), "")
			g.add(dt(1), -31000, "WEG Rheinstrasse 12", "DE66510500150400500600", "Hausgeld "+mm+" Whg. 4", "")
			g.add(dt(12), -7000, "Wasserverband Musterstadt", "DE10512500000000556677", "Abschlag Wasser Talstr. 3", "")
			g.add(dt(15), -2500, "Stadtwerke Musterstadt", "DE43500500000001234567", "Allgemeinstrom Talstr. 3", "")
			g.add(dt(28), -50000, "Umbuchung Girokonto", demoIBANA, "Entnahme Überschuss", "")
			if mo%3 == 2 {
				g.add(dt(15), -18000, "Stadt Musterstadt", "DE12510500150000111222", "Grundsteuer B Talstr. 3 Kassenzeichen 4711", "")
				g.add(dt(15), -9500, "Landkreis Musterkreis", "DE45510500150000333444", "Abfallgebuehren Talstr. 3", "")
			}
			if mo == time.January {
				g.add(dt(10), -64000, "Allianz Versicherungs-AG", "DE27700800000123456789", "Wohngebaeudeversicherung Talstr. 3", "")
			}
			if mo == time.May {
				g.add(dt(20), -12000, "Bezirksschornsteinfeger Klein", "", "Kehr- und Messgebuehr Talstr. 3", "")
			}
			if g.r.Intn(4) == 0 {
				g.add(dt(10+g.r.Intn(15)), -g.between(15000, 90000), "Sanitaer Becker GmbH", "", "Rechnung Reparatur Talstr. 3", "")
			}
		case "demo-giro-b":
			g.add(workday(dt(lastDay), true), 210420+g.between(-300, 300), "Praxis Dr. Lehmann", "DE02120300000000202051", "Gehalt "+mm, "")
			g.add(workday(dt(1), false), -90000, "Gemeinschaftskonto", demoIBANJoint, "Haushalt "+mm, "")
			g.add(dt(18), -1500, "congstar", "", "congstar Rechnung "+mm, "")
			g.add(dt(7), -999, "Disney Plus", "", "Disney+ Monatsabo", "")
			if m.Before(today.AddDate(0, -5, 0)) {
				g.add(dt(11), -995, "Audible GmbH", "", "Audible Abo Monatsbeitrag", "")
			}
			g.add(dt(2), -3900, "Urban Sports GmbH", "", "Urban Sports Club Membership", "")
			if g.r.Intn(2) == 0 {
				g.add(dt(10+g.r.Intn(15)), -g.between(2999, 8999), "PayPal Europe S.a.r.l. et Cie S.C.A", "LU89751000135104200E", "Ihr Einkauf bei Zalando SE", "")
			}
			g.add(dt(14), -g.between(900, 3200), "ROSSMANN 1234//Nachbarstadt/DE", "", "Kartenzahlung", "")
			g.add(dt(19), -g.between(650, 1900), "Cafe am Markt//Nachbarstadt/DE", "", "Kartenzahlung", "")
			if g.r.Intn(3) == 0 {
				g.add(dt(9+g.r.Intn(15)), -g.between(1990, 7990), "DB Vertrieb GmbH", "", "Fahrkarte Online-Ticket", "")
			}
			if g.r.Intn(3) == 0 {
				g.add(dt(9+g.r.Intn(15)), -g.between(2990, 8990), "H&M Hennes & Mauritz//Beispielstadt/DE", "", "Kartenzahlung", "")
			}
		}
	}
	var out []Transaction
	for _, t := range g.out {
		bd, _ := time.Parse(apiDate, t.BookingDate)
		if !bd.After(today) {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].BookingDate < out[j].BookingDate })
	return out
}
