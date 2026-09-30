// Package api provides the REST API and serves the web UI.
package api

import (
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/AQUI74S/homestead/internal/config"
	eb "github.com/AQUI74S/homestead/internal/enablebanking"
	"github.com/AQUI74S/homestead/internal/store"
	"github.com/AQUI74S/homestead/internal/syncer"
)

type Server struct {
	cfg  config.Config
	st   *store.Store
	p    eb.Provider
	sync *syncer.Syncer
	log  *slog.Logger
	web  fs.FS

	banksMu    sync.Mutex // guards the bank list cache
	banksCache []eb.ASPSP
	banksAt    time.Time
}

func New(cfg config.Config, st *store.Store, p eb.Provider, sy *syncer.Syncer, log *slog.Logger, web fs.FS) *Server {
	return &Server{cfg: cfg, st: st, p: p, sync: sy, log: log, web: web}
}

// Handler returns the HTTP handler with all routes (see routes.go).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, rt := range s.routes() {
		h := http.Handler(s.handle(rt.handler))
		if !rt.public {
			h = s.auth(h)
		}
		mux.Handle(rt.pattern, h)
	}
	mux.Handle("GET /", s.static())
	return securityHeaders(mux)
}

func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		h.ServeHTTP(w, r)
	})
}

// static serves the web UI; unknown API paths get a JSON 404.
func (s *Server) static() http.Handler {
	files := http.FileServer(http.FS(s.web))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, apiPrefix) {
			writeJSON(w, http.StatusNotFound, errorResponse{"Unbekannter Endpunkt"})
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})
}
