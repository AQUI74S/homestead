package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/AQUI74S/homestead/internal/budget"
	"github.com/AQUI74S/homestead/internal/domain"
	"github.com/AQUI74S/homestead/internal/syncer"
)

type overviewResponse struct {
	*budget.Overview
	Sync  syncer.Status `json:"sync"`
	NameA string        `json:"name_a"`
	NameB string        `json:"name_b"`
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request) error {
	ov, err := budget.LoadOverview(r.Context(), s.st, r.URL.Query().Get("month"))
	if err != nil {
		return err
	}
	settings, _ := s.st.Settings(r.Context())
	return reply(w, overviewResponse{Overview: ov, Sync: s.sync.Status(),
		NameA: settings[domain.SettingNameA], NameB: settings[domain.SettingNameB]})
}

func (s *Server) categories(w http.ResponseWriter, r *http.Request) error {
	cats, err := s.st.Categories(r.Context())
	if err != nil {
		return err
	}
	return reply(w, cats)
}

func (s *Server) createCategory(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Group domain.Group
		Name  string
	}
	if err := decode(r, &in); err != nil || !in.Group.Valid() || strings.TrimSpace(in.Name) == "" {
		return badRequest("Bereich und Name angeben")
	}
	id, err := s.st.CreateCategory(r.Context(), in.Group, strings.TrimSpace(in.Name))
	if err != nil {
		return err
	}
	return withID(w, http.StatusCreated, id)
}

func (s *Server) setBudget(w http.ResponseWriter, r *http.Request) error {
	id, ok := pathID(r)
	var in struct {
		Amount int64  `json:"amount"` // cents
		Month  string `json:"month"`  // empty = default budget
	}
	if !ok || decode(r, &in) != nil || in.Amount < 0 {
		return badRequest("Betrag in Cent angeben")
	}
	if in.Month != "" && !domain.ValidMonth(in.Month) {
		return badRequest("Monat im Format JJJJ-MM angeben")
	}
	if err := s.st.SetBudget(r.Context(), id, in.Month, in.Amount); err != nil {
		return err
	}
	return okReply(w)
}

func (s *Server) suggestBudgets(w http.ResponseWriter, r *http.Request) error {
	p, err := budget.LoadPeriods(r.Context(), s.st)
	if err != nil {
		return err
	}
	var in struct {
		Overwrite bool `json:"overwrite"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in) // empty body = only missing budgets
	n, months, err := budget.SuggestBudgets(r.Context(), s.st, p, time.Now(), in.Overwrite)
	if err != nil {
		return err
	}
	return reply(w, map[string]int{"updated": n, "months": months})
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		NameA        string            `json:"name_a"`
		NameB        string            `json:"name_b"`
		OwnNames     *string           `json:"own_names"`     // additional own names, comma-separated
		PeriodMode   domain.PeriodMode `json:"period_mode"`   // salary | calendar
		SalarySeries *string           `json:"salary_series"` // comma-separated series IDs, "" = automatic
	}
	if err := decode(r, &in); err != nil {
		return badRequest("Ungültige Anfrage")
	}
	ctx := r.Context()
	if in.PeriodMode != "" {
		if in.PeriodMode != domain.PeriodSalary && in.PeriodMode != domain.PeriodCalendar {
			return badRequest("Budgetmonat: salary oder calendar")
		}
		if err := s.st.SetSetting(ctx, domain.SettingPeriodMode, string(in.PeriodMode)); err != nil {
			return err
		}
	}
	if in.SalarySeries != nil {
		var ids []string
		for _, id := range budget.ParseSeriesIDs(*in.SalarySeries) {
			ids = append(ids, strconv.FormatInt(id, 10))
		}
		if strings.TrimSpace(*in.SalarySeries) != "" && len(ids) == 0 {
			return badRequest("Ungültige Gehaltsserie")
		}
		if err := s.st.SetSetting(ctx, domain.SettingSalarySeries, strings.Join(ids, ",")); err != nil {
			return err
		}
	}
	if in.OwnNames != nil {
		if err := s.st.SetSetting(ctx, domain.SettingOwnNames, strings.TrimSpace(*in.OwnNames)); err != nil {
			return err
		}
		if err := s.sync.Reclassify(ctx); err != nil {
			return err
		}
	}
	for key, v := range map[string]string{domain.SettingNameA: in.NameA, domain.SettingNameB: in.NameB} {
		if v = strings.TrimSpace(v); v != "" {
			if err := s.st.SetSetting(ctx, key, v); err != nil {
				return err
			}
		}
	}
	return okReply(w)
}
