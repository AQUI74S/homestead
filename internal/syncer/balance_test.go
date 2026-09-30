package syncer

import (
	"testing"

	eb "github.com/AQUI74S/homestead/internal/enablebanking"
)

func bal(typ, amount, ref string) eb.Balance {
	return eb.Balance{BalanceType: typ, ReferenceDate: ref, BalanceAmount: eb.Amount{Currency: "EUR", Amount: amount}}
}

func TestPickBalance(t *testing.T) {
	cases := []struct {
		name string
		in   []eb.Balance
		want int64
	}{
		{"newer interim beats yesterday's closing balance",
			[]eb.Balance{bal("CLBD", "1839.08", "2026-09-29"), bal("ITBD", "1074.95", "2026-09-30")}, 107495},
		{"same day: interim booked before closing booked",
			[]eb.Balance{bal("CLBD", "100.00", "2026-09-30"), bal("ITBD", "90.00", "2026-09-30")}, 9000},
		{"same day: booked before available",
			[]eb.Balance{bal("ITAV", "5100.00", "2026-09-30"), bal("CLBD", "100.00", "2026-09-30")}, 10000},
		{"unknown type only", []eb.Balance{bal("OTHR", "12.34", "")}, 1234},
	}
	for _, c := range cases {
		got, ok := pickBalance(c.in)
		if !ok || got != c.want {
			t.Errorf("%s: got %d (%v), want %d", c.name, got, ok, c.want)
		}
	}
	if _, ok := pickBalance(nil); ok {
		t.Error("empty list must not return a balance")
	}
}
