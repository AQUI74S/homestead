// Package config loads the runtime configuration from environment variables.
//
// All application settings use the HS_ prefix (e.g. HS_PUBLIC_URL). The legacy
// HK_ prefix is still accepted so existing installations keep working.
// Enable Banking settings use the EB_ prefix.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	Addr         string        // HTTP listen address, e.g. ":8080"
	PublicURL    string        // public base URL, e.g. https://homestead.example.com (used for the bank redirect)
	DatabaseURL  string        // Postgres DSN
	Password     string        // login password for the web UI (empty = no login, local testing only)
	SessionKey   string        // secret used to sign login cookies
	SyncInterval time.Duration // interval between automatic bank syncs
	Demo         bool          // use the simulated demo bank instead of Enable Banking

	EBAppID      string // Enable Banking application ID (kid in the JWT)
	EBKeyPath    string // path to the private RSA key (PEM)
	EBAPIBase    string // default: https://api.enablebanking.com
	EBCountry    string // default country for the bank list
	EBConsentMax time.Duration
}

func Load() (Config, error) {
	c := Config{
		Addr:         setting("ADDR", ":8080"),
		PublicURL:    strings.TrimRight(setting("PUBLIC_URL", "http://localhost:8080"), "/"),
		DatabaseURL:  setting("DATABASE_URL", "postgres://homestead:homestead@localhost:5432/homestead?sslmode=disable"),
		Password:     setting("PASSWORD", ""),
		SessionKey:   setting("SESSION_KEY", ""),
		Demo:         isTrue(setting("DEMO", "")),
		EBAppID:      env("EB_APP_ID", ""),
		EBKeyPath:    env("EB_PRIVATE_KEY", "/run/secrets/enablebanking.pem"),
		EBAPIBase:    strings.TrimRight(env("EB_API_BASE", "https://api.enablebanking.com"), "/"),
		EBCountry:    env("EB_COUNTRY", "DE"),
		EBConsentMax: 180 * 24 * time.Hour,
	}
	var err error
	if c.SyncInterval, err = time.ParseDuration(setting("SYNC_INTERVAL", "6h")); err != nil {
		return c, fmt.Errorf("HS_SYNC_INTERVAL: %w", err)
	}
	if c.SyncInterval < time.Hour {
		// Without the user present, PSD2 allows only 4 fetches per day and account.
		c.SyncInterval = time.Hour
	}
	if c.Password != "" && len(c.SessionKey) < 32 {
		return c, fmt.Errorf("HS_SESSION_KEY must be at least 32 characters long when HS_PASSWORD is set")
	}
	if !c.Demo && c.EBAppID == "" {
		return c, fmt.Errorf("EB_APP_ID is missing (or set HS_DEMO=true to use the demo bank)")
	}
	return c, nil
}

// setting reads HS_<name>, falling back to the legacy HK_<name>.
func setting(name, def string) string {
	if v := env("HS_"+name, ""); v != "" {
		return v
	}
	return env("HK_"+name, def)
}

func env(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func isTrue(v string) bool {
	switch strings.ToLower(v) {
	case "1", "true", "yes", "ja", "on":
		return true
	}
	return false
}
