package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/AQUI74S/homestead/internal/budget"
	"github.com/AQUI74S/homestead/internal/store"
)

// Request size limits.
const (
	maxJSONBody   = 1 << 20  // 1 MiB
	maxUploadBody = 20 << 20 // 20 MiB (CSV import)
)

// handlerFunc is an HTTP handler that returns an error instead of writing it.
// Errors are turned into JSON responses by Server.handle.
type handlerFunc func(w http.ResponseWriter, r *http.Request) error

// apiError is an error with an HTTP status and a message for the user.
type apiError struct {
	status int
	msg    string
}

func (e *apiError) Error() string { return e.msg }

func badRequest(msg string) error { return &apiError{http.StatusBadRequest, msg} }
func notFound(msg string) error   { return &apiError{http.StatusNotFound, msg} }

// handle adapts a handlerFunc to http.Handler and writes its error, if any.
func (s *Server) handle(h handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			s.writeError(w, r, err)
		}
	}
}

type errorResponse struct {
	Error string `json:"error"`
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var ae *apiError
	var me budget.ErrMonth
	switch {
	case errors.As(err, &ae):
		writeJSON(w, ae.status, errorResponse{ae.msg})
	case errors.As(err, &me):
		writeJSON(w, http.StatusBadRequest, errorResponse{me.Error()})
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorResponse{"Nicht gefunden"})
	default:
		s.log.Error("API error", "path", r.URL.Path, "err", err)
		writeJSON(w, http.StatusInternalServerError, errorResponse{err.Error()})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// reply writes v as JSON with status 200.
func reply(w http.ResponseWriter, v any) error {
	writeJSON(w, http.StatusOK, v)
	return nil
}

type okResponse struct {
	OK bool `json:"ok"`
}

// okReply confirms a change without further data.
func okReply(w http.ResponseWriter) error { return reply(w, okResponse{true}) }

type idResponse struct {
	ID int64 `json:"id"`
}

// withID returns the ID of a created or saved record.
func withID(w http.ResponseWriter, status int, id int64) error {
	writeJSON(w, status, idResponse{id})
	return nil
}

// decode reads a JSON request body.
func decode(r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxJSONBody)).Decode(v)
}

// pathID returns the {id} path parameter.
func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

// requireID returns the {id} path parameter or a 400 error.
func requireID(r *http.Request) (int64, error) {
	if id, ok := pathID(r); ok {
		return id, nil
	}
	return 0, badRequest("Ungültige ID")
}

// queryInt64 returns a numeric query parameter (0 if missing or invalid).
func queryInt64(r *http.Request, key string) int64 {
	v, _ := strconv.ParseInt(r.URL.Query().Get(key), 10, 64)
	return v
}

// queryYear returns the "year" query parameter or def for a missing or implausible value.
func queryYear(r *http.Request, def int) int {
	if y, err := strconv.Atoi(r.URL.Query().Get("year")); err == nil && y >= minYear {
		return y
	}
	return def
}

// minYear is the earliest plausible year in requests.
const minYear = 1900
