package csvimport

import "testing"

func TestSparkasseCAMT(t *testing.T) {
	// Windows-1252 encoded, two-digit year
	data := []byte("\"Auftragskonto\";\"Buchungstag\";\"Valutadatum\";\"Buchungstext\";\"Verwendungszweck\";\"Glaeubiger ID\";\"Mandatsreferenz\";\"Kundenreferenz (End-to-End)\";\"Sammlerreferenz\";\"Lastschrift Ursprungsbetrag\";\"Auslagenersatz Ruecklastschrift\";\"Beguenstigter/Zahlungspflichtiger\";\"Kontonummer/IBAN\";\"BIC (SWIFT-Code)\";\"Betrag\";\"Waehrung\";\"Info\"\r\n" +
		"\"DE00\";\"15.10.25\";\"15.10.25\";\"FOLGELASTSCHRIFT\";\"Kfz-Versicherung 2025 M\xfcller\";\"\";\"\";\"\";\"\";\"\";\"\";\"HUK-COBURG\";\"DE36783500000001234567\";\"X\";\"-612,00\";\"EUR\";\"Umsatz gebucht\"\r\n" +
		"\"DE00\";\"28.10.25\";\"28.10.25\";\"GUTSCHR. UEBERWEISUNG\";\"LOHN/GEHALT 10/2025\";\"\";\"\";\"\";\"\";\"\";\"\";\"Muster GmbH\";\"DE44\";\"X\";\"3.412,55\";\"EUR\";\"Umsatz gebucht\"\r\n")
	rows, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].AmountCents != -61200 || rows[1].AmountCents != 341255 || rows[0].Counterparty != "HUK-COBURG" {
		t.Fatalf("%+v", rows)
	}
	if rows[0].Remittance != "Kfz-Versicherung 2025 Müller" || rows[0].BookingDate.Format("2006-01-02") != "2025-10-15" {
		t.Errorf("remittance/date: %+v", rows[0])
	}
}

func TestDKBWithPreamble(t *testing.T) {
	data := []byte("\"Girokonto\";\"DE12 3456\"\n\"Zeitraum:\";\"01.01.2025 - 31.12.2025\"\n\"Kontostand vom 31.12.2025:\";\"1.234,56 €\"\n\n" +
		"\"Buchungsdatum\";\"Wertstellung\";\"Status\";\"Zahlungspflichtige*r\";\"Zahlungsempfänger*in\";\"Verwendungszweck\";\"Umsatztyp\";\"IBAN\";\"Betrag (€)\";\"Gläubiger-ID\";\"Mandatsreferenz\";\"Kundenreferenz\"\n" +
		"\"03.06.25\";\"03.06.25\";\"Gebucht\";\"Max\";\"ADAC e.V.\";\"Mitgliedschaft 2025\";\"Ausgang\";\"DE11\";\"-94\";\"\";\"\";\"\"\n")
	rows, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].AmountCents != -9400 || rows[0].Remittance != "Mitgliedschaft 2025" || rows[0].Counterparty != "ADAC e.V." {
		t.Fatalf("%+v", rows)
	}
}

func TestCommerzbank(t *testing.T) {
	data := []byte("Buchungstag;Wertstellung;Umsatzart;Buchungstext;Betrag;Währung;Auftraggeberkonto;Bankleitzahl Auftraggeberkonto;IBAN Auftraggeberkonto;Kategorie\n" +
		"14.03.2025;14.03.2025;Lastschrift;AMAZON EU S.A R.L. Prime Mitgliedschaft;-89,90;EUR;123;456;DE00;Shopping\n")
	rows, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].AmountCents != -8990 || rows[0].Remittance == "" {
		t.Fatalf("%+v", rows)
	}
}
