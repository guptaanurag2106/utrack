package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"time"

	"sort"
	"utrack/internal/rules"
	"utrack/internal/storage"
)

//go:embed static/*
var staticFS embed.FS

// Server hosts the on-demand local dashboard.
type Server struct {
	db    *storage.DB
	rules *rules.Config
	port  int
}

// NewServer initializes a dashboard HTTP server.
func NewServer(db *storage.DB, port int) *Server {
	if port <= 0 {
		port = 8080
	}
	return &Server{
		db:    db,
		rules: rules.Load(),
		port:  port,
	}
}

// Start begins listening and serving HTTP requests.
func (s *Server) Start() error {
	mux := http.NewServeMux()

	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return fmt.Errorf("sub static fs: %w", err)
	}

	fileServer := http.FileServer(http.FS(sub))
	mux.Handle("/static/", http.StripPrefix("/static/", fileServer))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, err := staticFS.ReadFile("static/index.html")
		if err != nil {
			http.Error(w, "index not found", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})

	mux.HandleFunc("/api/summary", s.handleSummary)
	mux.HandleFunc("/api/app-titles", s.handleAppTitles)
	mux.HandleFunc("/api/timeline", s.handleTimeline)
	mux.HandleFunc("/api/daily", s.handleDaily)
	mux.HandleFunc("/api/bounds", s.handleBounds)
	mux.HandleFunc("/api/media", s.handleMedia)
	mux.HandleFunc("/api/media-timeline", s.handleMediaTimeline)

	addr := fmt.Sprintf("127.0.0.1:%d", s.port)
	log.Printf("[web] dashboard listening on http://%s", addr)
	return http.ListenAndServe(addr, mux)
}

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	from, to := parseRange(r)
	summary, err := s.db.GetSummary(from, to)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, summary)
}

func (s *Server) handleAppTitles(w http.ResponseWriter, r *http.Request) {
	appID := r.URL.Query().Get("app")
	from, to := parseRange(r)
	rawTitles, err := s.db.GetAppTitles(appID, from, to)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	grouped := make(map[string]int64)
	for _, t := range rawTitles {
		label := s.rules.CategorizeTitle(appID, t.Title)
		grouped[label] += t.DurationS
	}

	result := make([]storage.TitleSummary, 0, len(grouped))
	for label, dur := range grouped {
		result = append(result, storage.TitleSummary{Title: label, DurationS: dur})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].DurationS > result[j].DurationS
	})

	writeJSON(w, result)
}

func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	from, to := parseRange(r)
	limit := 1000
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
			limit = l
		}
	}

	timeline, err := s.db.GetTimeline(from, to, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, timeline)
}

func (s *Server) handleDaily(w http.ResponseWriter, r *http.Request) {
	from, to := parseRange(r)
	daily, err := s.db.GetDailyBreakdown(from, to)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, daily)
}

func (s *Server) handleBounds(w http.ResponseWriter, r *http.Request) {
	bounds, err := s.db.GetTimeBounds()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, bounds)
}

func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request) {
	from, to := parseRange(r)
	summary, err := s.db.GetMediaSummary(from, to)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, summary)
}

func (s *Server) handleMediaTimeline(w http.ResponseWriter, r *http.Request) {
	from, to := parseRange(r)
	limit := 1000
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
			limit = l
		}
	}

	timeline, err := s.db.GetMediaTimeline(from, to, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, timeline)
}

func parseRange(r *http.Request) (int64, int64) {
	now := time.Now().Unix()
	from := now - 86400
	to := now + 86400

	if fStr := r.URL.Query().Get("from"); fStr != "" {
		if val, err := strconv.ParseInt(fStr, 10, 64); err == nil {
			from = val
		}
	}
	if tStr := r.URL.Query().Get("to"); tStr != "" {
		if val, err := strconv.ParseInt(tStr, 10, 64); err == nil {
			to = val
		}
	}
	return from, to
}

func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if data == nil {
		_, _ = w.Write([]byte("[]"))
		return
	}
	_ = json.NewEncoder(w).Encode(data)
}
