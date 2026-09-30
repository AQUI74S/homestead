package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/AQUI74S/homestead/internal/budget"
	"github.com/AQUI74S/homestead/internal/domain"
)

const (
	sessionCookie   = "homestead_session"
	sessionLifetime = 30 * 24 * time.Hour
	// loginDelay slows down password guessing.
	loginDelay = 700 * time.Millisecond
)

// sign returns the cookie value for a session that expires at exp (Unix time).
func (s *Server) sign(exp int64) string {
	m := hmac.New(sha256.New, []byte(s.cfg.SessionKey))
	fmt.Fprintf(m, "%d", exp)
	return fmt.Sprintf("%d.%s", exp, hex.EncodeToString(m.Sum(nil)))
}

func (s *Server) loggedIn(r *http.Request) bool {
	if s.cfg.Password == "" {
		return true
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	expStr, _, ok := strings.Cut(c.Value, ".")
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if !ok || err != nil || time.Now().Unix() > exp {
		return false
	}
	return hmac.Equal([]byte(c.Value), []byte(s.sign(exp)))
}

// auth rejects requests without a valid session.
func (s *Server) auth(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.loggedIn(r) {
			if r.URL.Path == callbackPath { // browser returning from the bank
				http.Redirect(w, r, "/#login", http.StatusFound)
				return
			}
			writeJSON(w, http.StatusUnauthorized, errorResponse{"Bitte anmelden"})
			return
		}
		h.ServeHTTP(w, r)
	})
}

type meResponse struct {
	LoggedIn     bool   `json:"logged_in"`
	AuthRequired bool   `json:"auth_required"`
	Demo         bool   `json:"demo"`
	NameA        string `json:"name_a"`
	NameB        string `json:"name_b"`
	CurrentMonth string `json:"current_month"`
	// only for logged-in users
	PeriodMode       *string `json:"period_mode,omitempty"`
	SalarySeries     *string `json:"salary_series,omitempty"`
	SalarySeriesName *string `json:"salary_series_name,omitempty"`
	OwnNames         *string `json:"own_names,omitempty"`
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) error {
	settings, _ := s.st.Settings(r.Context())
	out := meResponse{
		LoggedIn:     s.loggedIn(r),
		AuthRequired: s.cfg.Password != "",
		Demo:         s.cfg.Demo,
		NameA:        settings[domain.SettingNameA],
		NameB:        settings[domain.SettingNameB],
		CurrentMonth: time.Now().Format(domain.MonthLayout),
	}
	if out.LoggedIn {
		if p, err := budget.LoadPeriods(r.Context(), s.st); err == nil {
			str := func(v string) *string { return &v }
			out.CurrentMonth = p.Current(time.Now())
			out.PeriodMode = str(settings[domain.SettingPeriodMode])
			out.SalarySeries = str(settings[domain.SettingSalarySeries])
			out.SalarySeriesName = str(p.SeriesName)
			out.OwnNames = str(settings[domain.SettingOwnNames])
		}
	}
	return reply(w, out)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) error {
	var in struct{ Password string }
	if err := decode(r, &in); err != nil {
		return badRequest("Ungültige Anfrage")
	}
	if s.cfg.Password == "" || subtle.ConstantTimeCompare([]byte(in.Password), []byte(s.cfg.Password)) != 1 {
		time.Sleep(loginDelay)
		return &apiError{http.StatusUnauthorized, "Passwort stimmt nicht"}
	}
	exp := time.Now().Add(sessionLifetime).Unix()
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: s.sign(exp), Path: "/", HttpOnly: true,
		Secure: strings.HasPrefix(s.cfg.PublicURL, "https://"), SameSite: http.SameSiteLaxMode, Expires: time.Unix(exp, 0)})
	return okReply(w)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) error {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	return okReply(w)
}
