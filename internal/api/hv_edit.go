package api

import (
	"net/http"
	"strings"

	"github.com/AQUI74S/homestead/internal/domain"
	"github.com/AQUI74S/homestead/internal/hv"
	"github.com/AQUI74S/homestead/internal/store"
)

// Property management: properties, units, leases, rent steps and reminders.

func (s *Server) hvSaveProperty(w http.ResponseWriter, r *http.Request) error {
	var p store.Property
	if err := decode(r, &p); err != nil || strings.TrimSpace(p.Name) == "" {
		return badRequest("Name des Objekts angeben")
	}
	if id, ok := pathID(r); ok {
		p.ID = id
	}
	id, err := s.st.SaveProperty(r.Context(), p)
	if err != nil {
		return err
	}
	return withID(w, http.StatusOK, id)
}

func (s *Server) hvSaveUnit(w http.ResponseWriter, r *http.Request) error {
	var u store.Unit
	if err := decode(r, &u); err != nil || strings.TrimSpace(u.Name) == "" {
		return badRequest("Name der Einheit angeben")
	}
	if id, ok := pathID(r); ok {
		u.ID = id
	} else if u.PropertyID == 0 {
		return badRequest("Objekt fehlt")
	}
	id, err := s.st.SaveUnit(r.Context(), u)
	if err != nil {
		return err
	}
	return withID(w, http.StatusOK, id)
}

func (s *Server) hvSaveLease(w http.ResponseWriter, r *http.Request) error {
	var l store.Lease
	if err := decode(r, &l); err != nil || strings.TrimSpace(l.Tenant.Name) == "" || l.UnitID == 0 {
		return badRequest("Mieter und Einheit angeben")
	}
	if !domain.ValidDate(l.Start) {
		return badRequest("Mietbeginn angeben")
	}
	if l.RentType == "" {
		l.RentType = hv.RentFixed
	}
	if l.DueDay < 1 || l.DueDay > hv.MaxDueDay {
		l.DueDay = hv.DefaultDueDay
	}
	if l.Persons < 0 {
		l.Persons = 0
	}
	if id, ok := pathID(r); ok {
		l.ID = id
	}
	id, err := s.st.SaveLease(r.Context(), l)
	if err != nil {
		return err
	}
	if err := hv.Reassign(r.Context(), s.st); err != nil {
		return err
	}
	return withID(w, http.StatusOK, id)
}

func (s *Server) hvAddStep(w http.ResponseWriter, r *http.Request) error {
	id, ok := pathID(r)
	var st store.RentStep
	if !ok || decode(r, &st) != nil {
		return badRequest("Ungültige Anfrage")
	}
	if !domain.ValidDate(st.ValidFrom) || st.RentCold <= 0 {
		return badRequest("Datum und neue Kaltmiete angeben")
	}
	st.LeaseID = id
	sid, err := s.st.AddRentStep(r.Context(), st)
	if err != nil {
		return err
	}
	return withID(w, http.StatusOK, sid)
}

func (s *Server) hvAddReminder(w http.ResponseWriter, r *http.Request) error {
	var rm store.Reminder
	if err := decode(r, &rm); err != nil || strings.TrimSpace(rm.Title) == "" {
		return badRequest("Titel und Datum angeben")
	}
	if !domain.ValidDate(rm.DueDate) {
		return badRequest("Datum angeben")
	}
	id, err := s.st.AddReminder(r.Context(), rm)
	if err != nil {
		return err
	}
	return withID(w, http.StatusOK, id)
}

func (s *Server) hvReminderDone(w http.ResponseWriter, r *http.Request) error {
	id, ok := pathID(r)
	var in struct{ Done bool }
	if !ok || decode(r, &in) != nil {
		return badRequest("Ungültige Anfrage")
	}
	if err := s.st.SetReminderDone(r.Context(), id, in.Done); err != nil {
		return err
	}
	return okReply(w)
}
