// Package web serves the JSON API (and, later, the dashboard) from the store.
package web

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/tlugger/wtwlt/server/internal/store"
)

type Server struct {
	store *store.Store
}

func New(st *store.Store) *Server { return &Server{store: st} }

// Handler returns the HTTP routes (stdlib mux; Go 1.22+ method+path patterns).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /api/current", s.current)
	mux.HandleFunc("GET /api/stations", s.stations)
	return mux
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("ok\n"))
}

// GET /api/current?station=wtwlt-01 — latest reading for a station.
func (s *Server) current(w http.ResponseWriter, r *http.Request) {
	station := r.URL.Query().Get("station")
	if station == "" {
		station = "wtwlt-01" // default single-station deployment
	}
	reading, _, err := s.store.LatestReading(station)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "no readings for station", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, reading)
}

// GET /api/stations — status of all known stations.
func (s *Server) stations(w http.ResponseWriter, _ *http.Request) {
	stations, err := s.store.Stations()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if stations == nil {
		stations = []store.StationStatus{}
	}
	writeJSON(w, stations)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
