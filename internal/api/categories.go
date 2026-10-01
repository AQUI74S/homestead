package api

import (
	"net/http"
	"strings"

	"github.com/AQUI74S/homestead/internal/classify"
	"github.com/AQUI74S/homestead/internal/domain"
)

const (
	maxCategoryName = 60 // characters
	minRulePattern  = 3  // characters, so that a rule doesn't catch everything
)

func (s *Server) categories(w http.ResponseWriter, r *http.Request) error {
	cats, err := s.st.Categories(r.Context())
	if err != nil {
		return err
	}
	return reply(w, cats)
}

type categoryInput struct {
	Group domain.Group `json:"group"`
	Name  string       `json:"name"`
}

// checkName validates a category name that no other category (except id) uses.
func (s *Server) checkName(r *http.Request, name string, except int64) error {
	if name == "" || len([]rune(name)) > maxCategoryName {
		return badRequest("Name mit 1 bis 60 Zeichen angeben")
	}
	taken, err := s.st.CategoryNameTaken(r.Context(), name, except)
	if err != nil {
		return err
	}
	if taken {
		return badRequest("Eine Kategorie „" + name + "“ gibt es schon")
	}
	return nil
}

func (s *Server) createCategory(w http.ResponseWriter, r *http.Request) error {
	var in categoryInput
	if err := decode(r, &in); err != nil || !in.Group.ForCustomCategory() {
		return badRequest("Bereich und Name angeben")
	}
	name := strings.TrimSpace(in.Name)
	if err := s.checkName(r, name, 0); err != nil {
		return err
	}
	id, err := s.st.CreateCategory(r.Context(), in.Group, name)
	if err != nil {
		return err
	}
	return withID(w, http.StatusCreated, id)
}

// patchCategory renames a category; own categories can also change their group.
func (s *Server) patchCategory(w http.ResponseWriter, r *http.Request) error {
	id, err := requireID(r)
	if err != nil {
		return err
	}
	var in categoryInput
	if decode(r, &in) != nil {
		return badRequest("Ungültige Anfrage")
	}
	c, err := s.st.CategoryByID(r.Context(), id)
	if err != nil {
		return err
	}
	name, group := c.Name, c.Group
	if in.Name != "" {
		name = strings.TrimSpace(in.Name)
		if err := s.checkName(r, name, id); err != nil {
			return err
		}
	}
	if in.Group != "" && in.Group != c.Group {
		if !c.Custom {
			return badRequest("Eingebaute Kategorien bleiben in ihrem Bereich")
		}
		if !in.Group.ForCustomCategory() {
			return badRequest("Unbekannter Bereich")
		}
		group = in.Group
	}
	if err := s.st.UpdateCategory(r.Context(), id, name, group); err != nil {
		return err
	}
	return okReply(w)
}

type releasedResponse struct {
	Released int64 `json:"released"` // transactions classified anew
}

// deleteCategory removes an own category; its transactions are classified anew.
func (s *Server) deleteCategory(w http.ResponseWriter, r *http.Request) error {
	id, err := requireID(r)
	if err != nil {
		return err
	}
	c, err := s.st.CategoryByID(r.Context(), id)
	if err != nil {
		return err
	}
	if !c.Custom {
		return badRequest("Eingebaute Kategorien lassen sich nur umbenennen")
	}
	released, err := s.st.DeleteCategory(r.Context(), id)
	if err != nil {
		return err
	}
	if err := s.sync.Reclassify(r.Context()); err != nil {
		return err
	}
	return reply(w, releasedResponse{released})
}

func (s *Server) rules(w http.ResponseWriter, r *http.Request) error {
	rules, err := s.st.Rules(r.Context())
	if err != nil {
		return err
	}
	hits, err := s.st.RuleHits(r.Context())
	if err != nil {
		return err
	}
	for i, x := range rules {
		rules[i].Hits = hits[classify.RuleReason(classify.Rule{Field: x.Field, Pattern: x.Pattern})]
	}
	return reply(w, rules)
}

type hitsResponse struct {
	Hits int `json:"hits"` // transactions the rule categorizes
}

// createRule adds a rule, or changes the category of the rule with the same
// field and text, and classifies all transactions anew.
func (s *Server) createRule(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Field      domain.RuleField `json:"field"`
		Pattern    string           `json:"pattern"`
		CategoryID int64            `json:"category_id"`
	}
	if err := decode(r, &in); err != nil || !in.Field.Valid() {
		return badRequest("Wähle, worauf die Regel schaut")
	}
	pattern := strings.ToLower(strings.TrimSpace(in.Pattern))
	if in.Field == domain.RuleIBAN {
		pattern = strings.ToLower(domain.NormIBAN(pattern))
	}
	if len([]rune(pattern)) < minRulePattern {
		return badRequest("Suchtext mit mindestens 3 Zeichen angeben")
	}
	ctx := r.Context()
	if _, err := s.st.CategoryByID(ctx, in.CategoryID); err != nil {
		return badRequest("Kategorie wählen")
	}
	if err := s.st.UpsertRule(ctx, in.Field, pattern, in.CategoryID); err != nil {
		return err
	}
	if err := s.st.ReleaseToRule(ctx, in.Field, pattern, in.CategoryID); err != nil {
		return err
	}
	if err := s.sync.Reclassify(ctx); err != nil {
		return err
	}
	hits, err := s.st.RuleHits(ctx)
	if err != nil {
		return err
	}
	return reply(w, hitsResponse{hits[classify.RuleReason(classify.Rule{Field: in.Field, Pattern: pattern})]})
}

func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request) error {
	id, err := requireID(r)
	if err != nil {
		return err
	}
	if err := s.st.DeleteRule(r.Context(), id); err != nil {
		return err
	}
	if err := s.sync.Reclassify(r.Context()); err != nil {
		return err
	}
	return okReply(w)
}
