package store

import (
	"context"
	"strings"

	"github.com/AQUI74S/homestead/internal/domain"
)

// Settings holds the key/value pairs of the settings table (keys: domain.Setting*).
type Settings map[string]string

// PeriodMode returns the configured budget period mode (default: salary).
func (s Settings) PeriodMode() domain.PeriodMode {
	if domain.PeriodMode(s[domain.SettingPeriodMode]) == domain.PeriodCalendar {
		return domain.PeriodCalendar
	}
	return domain.PeriodSalary
}

// OwnNames returns the extra own names the user entered.
func (s Settings) OwnNames() []string {
	var out []string
	for _, n := range strings.Split(s[domain.SettingOwnNames], ",") {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return out
}

func (s *Store) Settings(ctx context.Context) (Settings, error) {
	type kv struct{ k, v string }
	rows, err := queryAll(ctx, s.DB, func(sc scanner) (kv, error) {
		var r kv
		return r, sc.Scan(&r.k, &r.v)
	}, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	m := Settings{}
	for _, r := range rows {
		m[r.k] = r.v
	}
	return m, nil
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES ($1,$2) ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, key, value)
	return err
}
