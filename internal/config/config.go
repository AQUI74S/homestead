// Package config loads the runtime configuration from environment variables.
//
// All application settings use the HS_ prefix (e.g. HS_PUBLIC_URL). The legacy
// HK_ prefix is still accepted so existing installations keep working.
// Enable Banking settings use the EB_ prefix.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr           string        // HTTP listen address, e.g. ":8080"
	PublicURL      string        // public base URL, e.g. https://homestead.example.com (used for the bank redirect)
	DatabaseURL    string        // Postgres DSN
	Password       string        // login password for the web UI (empty = no login, local testing only)
	SessionKey     string        // secret used to sign login cookies
	SyncInterval   time.Duration // minimum interval between automatic syncs of an account
	BankDailyLimit int           // unattended bank requests per account and 24 h (PSD2: usually 4)
	Demo           bool          // use the simulated demo bank instead of Enable Banking

	EBAppID      string // Enable Banking application ID (kid in the JWT)
	EBKeyPath    string // path to the private RSA key (PEM)
	EBAPIBase    string // default: https://api.enablebanking.com
	EBCountry    string // default country for the bank list
	EBConsentMax time.Duration
}

// Defaults for settings that are not configured.
const (
	defaultAddr         = ":8080"
	defaultPublicURL    = "http://localhost:8080"
	defaultDatabaseURL  = "postgres://homestead:homestead@localhost:5432/homestead?sslmode=disable"
	defaultSyncInterval = "6h"
	defaultDailyLimit   = "4" // PSD2: 4 unattended requests per account and day
	defaultEBKeyPath    = "/run/secrets/enablebanking.pem"
	defaultEBAPIBase    = "https://api.enablebanking.com"
	defaultEBCountry    = "DE"

	// consentMax is the longest bank consent requested (PSD2 allows 180 days).
	consentMax = 180 * 24 * time.Hour
	// minSyncInterval: syncing more often would use up the banks' daily limits.
	minSyncInterval = time.Hour
	// minDailyLimit: one sync needs two requests (transactions and balances).
	minDailyLimit = 2
	// minSessionKeyLen is the minimum length of the cookie signing key.
	minSessionKeyLen = 32
)

func Load() (Config, error) {
	c := Config{
		Addr:         setting("ADDR", defaultAddr),
		PublicURL:    strings.TrimRight(setting("PUBLIC_URL", defaultPublicURL), "/"),
		DatabaseURL:  setting("DATABASE_URL", defaultDatabaseURL),
		Password:     setting("PASSWORD", ""),
		SessionKey:   setting("SESSION_KEY", ""),
		Demo:         isTrue(setting("DEMO", "")),
		EBAppID:      env("EB_APP_ID", ""),
		EBKeyPath:    env("EB_PRIVATE_KEY", defaultEBKeyPath),
		EBAPIBase:    strings.TrimRight(env("EB_API_BASE", defaultEBAPIBase), "/"),
		EBCountry:    env("EB_COUNTRY", defaultEBCountry),
		EBConsentMax: consentMax,
	}
	var err error
	if c.SyncInterval, err = time.ParseDuration(setting("SYNC_INTERVAL", defaultSyncInterval)); err != nil {
		return c, fmt.Errorf("HS_SYNC_INTERVAL: %w", err)
	}
	if c.BankDailyLimit, err = strconv.Atoi(setting("BANK_DAILY_LIMIT", defaultDailyLimit)); err != nil || c.BankDailyLimit < minDailyLimit {
		return c, fmt.Errorf("HS_BANK_DAILY_LIMIT: whole number of at least %d expected", minDailyLimit)
	}
	if c.SyncInterval < minSyncInterval {
		c.SyncInterval = minSyncInterval
	}
	if c.Password != "" && len(c.SessionKey) < minSessionKeyLen {
		return c, fmt.Errorf("HS_SESSION_KEY must be at least %d characters long when HS_PASSWORD is set", minSessionKeyLen)
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
