package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/AQUI74S/homestead/internal/domain"
	eb "github.com/AQUI74S/homestead/internal/enablebanking"
	"github.com/AQUI74S/homestead/internal/store"
	"github.com/AQUI74S/homestead/internal/syncer"
)

const (
	// bankListTTL: the list of banks changes rarely and is cached.
	bankListTTL = 12 * time.Hour
	// consentMargin: the consent requested ends this long before the bank's maximum.
	consentMargin = time.Hour
	// stateBytes is the length of the random OAuth state.
	stateBytes    = 24
	secondsPerDay = 24 * 60 * 60
)

// Where the user lands after the bank authorization, with ?bank=<result>.
const (
	bankResultOK        = "ok"
	bankResultCancelled = "abgebrochen"
	bankResultError     = "fehler"
	pageAccounts        = "#konten"
	pageHVAccounts      = "#hv-konten"
)

func (s *Server) listBanks(ctx context.Context) ([]eb.ASPSP, error) {
	s.banksMu.Lock()
	defer s.banksMu.Unlock()
	if s.banksCache != nil && time.Since(s.banksAt) < bankListTTL {
		return s.banksCache, nil
	}
	banks, err := s.p.ASPSPs(ctx, s.cfg.EBCountry)
	if err != nil {
		return nil, err
	}
	sort.Slice(banks, func(i, j int) bool { return strings.ToLower(banks[i].Name) < strings.ToLower(banks[j].Name) })
	s.banksCache, s.banksAt = banks, time.Now()
	return banks, nil
}

type bankResponse struct {
	Name    string `json:"name"`
	Country string `json:"country"`
	Logo    string `json:"logo"`
	Beta    bool   `json:"beta"`
	Days    int64  `json:"consent_days"`
}

func (s *Server) banks(w http.ResponseWriter, r *http.Request) error {
	banks, err := s.listBanks(r.Context())
	if err != nil {
		return err
	}
	out := []bankResponse{}
	for _, b := range banks {
		out = append(out, bankResponse{b.Name, b.Country, b.Logo, b.Beta, b.MaximumConsentValidity / secondsPerDay})
	}
	return reply(w, out)
}

type accountResponse struct {
	store.Account
	Schedule *syncer.Schedule `json:"schedule"`
}

func (s *Server) accounts(w http.ResponseWriter, r *http.Request) error {
	accts, err := s.st.Accounts(r.Context())
	if err != nil {
		return err
	}
	sched, _ := s.sync.Schedules(r.Context())
	out := []accountResponse{}
	for _, a := range accts {
		row := accountResponse{Account: a}
		if sc, ok := sched[a.ID]; ok {
			row.Schedule = &sc
		}
		out = append(out, row)
	}
	return reply(w, out)
}

func (s *Server) patchAccount(w http.ResponseWriter, r *http.Request) error {
	id, ok := pathID(r)
	var in struct {
		Owner       domain.Owner `json:"owner"`
		DisplayName string       `json:"display_name"`
		Active      *bool        `json:"active"`
		Book        domain.Book  `json:"book"`
	}
	if !ok || decode(r, &in) != nil || !in.Owner.Valid() {
		return badRequest("Inhaber muss A, B oder leer (gemeinsam) sein")
	}
	active := in.Active == nil || *in.Active
	if err := s.st.UpdateAccount(r.Context(), id, in.Owner, strings.TrimSpace(in.DisplayName), active, in.Book); err != nil {
		return err
	}
	if err := s.sync.Reclassify(r.Context()); err != nil { // re-evaluate transfers between the books
		return err
	}
	return okReply(w)
}

func (s *Server) connections(w http.ResponseWriter, r *http.Request) error {
	c, err := s.st.Connections(r.Context())
	if err != nil {
		return err
	}
	return reply(w, c)
}

func randomState() string {
	b := make([]byte, stateBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// startConnection begins the authorization at a bank and returns the URL to send the user to.
func (s *Server) startConnection(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Bank    string      `json:"bank"`
		Country string      `json:"country"`
		Book    domain.Book `json:"book"`
	}
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Bank) == "" {
		return badRequest("Bank auswählen")
	}
	if in.Country == "" {
		in.Country = s.cfg.EBCountry
	}
	ctx := r.Context()
	valid := s.cfg.EBConsentMax
	if banks, err := s.listBanks(ctx); err == nil {
		for _, b := range banks {
			if b.Name == in.Bank && b.Country == in.Country && b.MaximumConsentValidity > 0 {
				if d := time.Duration(b.MaximumConsentValidity) * time.Second; d < valid {
					valid = d
				}
			}
		}
	}
	state := randomState()
	if _, err := s.st.CreatePendingConnection(ctx, in.Bank, in.Country, state, in.Book); err != nil {
		return err
	}
	u, err := s.p.StartAuth(ctx, eb.AuthRequest{ASPSPName: in.Bank, ASPSPCountry: in.Country, State: state,
		RedirectURL: s.cfg.PublicURL + callbackPath, ValidUntil: time.Now().Add(valid - consentMargin)})
	if err != nil {
		return err
	}
	return reply(w, map[string]string{"url": u})
}

// callback is where the bank sends the user back; it redirects to the accounts page.
func (s *Server) callback(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	target := pageAccounts
	if c, err := s.st.ConnectionByState(r.Context(), q.Get("state")); err == nil && c.Book == domain.BookProperty {
		target = pageHVAccounts
	}
	back := func(result string) error {
		http.Redirect(w, r, "/?"+url.Values{"bank": {result}}.Encode()+target, http.StatusFound)
		return nil
	}
	if e := q.Get("error"); e != "" {
		if c, err := s.st.ConnectionByState(r.Context(), q.Get("state")); err == nil {
			_ = s.st.SetConnectionStatus(r.Context(), c.ID, domain.ConnError, e+": "+q.Get("error_description"))
		}
		return back(bankResultCancelled)
	}
	if q.Get("code") == "" || q.Get("state") == "" {
		return back(bankResultError)
	}
	psu := eb.PSUFromRequest(r)
	if _, err := s.sync.CompleteAuth(r.Context(), q.Get("state"), q.Get("code"), &psu); err != nil {
		s.log.Error("bank authorization failed", "err", err)
		return back(bankResultError)
	}
	return back(bankResultOK)
}

func (s *Server) deleteConnection(w http.ResponseWriter, r *http.Request) error {
	id, err := requireID(r)
	if err != nil {
		return err
	}
	c, err := s.st.Connection(r.Context(), id)
	if err != nil {
		return err
	}
	if c.SessionID != "" {
		if err := s.p.DeleteSession(r.Context(), c.SessionID); err != nil && !eb.SessionInvalid(err) {
			s.log.Warn("session not deleted at Enable Banking", "err", err)
		}
	}
	if err := s.st.SetConnectionStatus(r.Context(), id, domain.ConnRevoked, "Vom Nutzer getrennt"); err != nil {
		return err
	}
	return okReply(w)
}

func (s *Server) triggerSync(w http.ResponseWriter, r *http.Request) error {
	psu := eb.PSUFromRequest(r) // the user clicked "sync now": requests are user-initiated
	writeJSON(w, http.StatusAccepted, map[string]bool{"started": s.sync.TriggerAsync(&psu)})
	return nil
}

func (s *Server) syncStatus(w http.ResponseWriter, r *http.Request) error {
	return reply(w, s.sync.Status())
}
