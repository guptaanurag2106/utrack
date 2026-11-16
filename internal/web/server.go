package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"sync"
	"time"

	"utrack/internal/rules"
	"utrack/internal/storage"
)

//go:embed static/*
var staticFS embed.FS

// Server hosts the on-demand local dashboard.
type Server struct {
	db       *storage.DB
	rulesMu  sync.RWMutex
	rules    *rules.Config
	rulesMod time.Time
	port     int
}

// NewServer initializes a dashboard HTTP server.
func NewServer(db *storage.DB, port int) *Server {
	if port <= 0 {
		port = 8080
	}
	s := &Server{
		db:    db,
		rules: rules.Load(),
		port:  port,
	}
	if fi, err := os.Stat(rules.GetConfigPath()); err == nil {
		s.rulesMod = fi.ModTime()
	}
	return s
}

func (s *Server) getRules() *rules.Config {
	cfgPath := rules.GetConfigPath()
	if fi, err := os.Stat(cfgPath); err == nil {
		s.rulesMu.RLock()
		modTime := s.rulesMod
		s.rulesMu.RUnlock()

		if fi.ModTime().After(modTime) {
			s.rulesMu.Lock()
			s.rules = rules.Load()
			s.rulesMod = fi.ModTime()
			s.rulesMu.Unlock()
		}
	}

	s.rulesMu.RLock()
	defer s.rulesMu.RUnlock()
	return s.rules
}

// Handler returns the configured HTTP handler with all routes registered.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	sub, err := fs.Sub(staticFS, "static")
	if err == nil {
		fileServer := http.FileServer(http.FS(sub))
		mux.Handle("/static/", http.StripPrefix("/static/", fileServer))
	}

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

	return mux
}

// Start begins listening and serving HTTP requests, optionally opening the default browser.
func (s *Server) Start(autoOpen bool) error {
	addr := fmt.Sprintf("127.0.0.1:%d", s.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer ln.Close()

	targetURL := fmt.Sprintf("http://%s", addr)
	log.Printf("[web] dashboard listening on %s", targetURL)

	if autoOpen {
		go openBrowser(targetURL)
	}

	return http.Serve(ln, s.Handler())
}

func openBrowser(targetURL string) {
	if browser := os.Getenv("BROWSER"); browser != "" {
		if err := exec.Command(browser, targetURL).Start(); err == nil {
			return
		}
	}
	_ = exec.Command("xdg-open", targetURL).Start()
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

	rCfg := s.getRules()
	grouped := make(map[string]int64)
	for _, t := range rawTitles {
		label := rCfg.CategorizeTitle(appID, t.Title)
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
	rawSessions, err := s.db.GetRawMediaSessions(from, to)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rCfg := s.getRules()
	type groupKey struct {
		category string
		playerID string
		label    string
	}
	grouped := make(map[groupKey]int64)

	for _, m := range rawSessions {
		cat, label := rCfg.CategorizeMedia(m.PlayerID, m.Title, m.URL)
		key := groupKey{category: cat, playerID: m.PlayerID, label: label}
		grouped[key] += m.DurationS
	}

	result := make([]storage.MediaSummary, 0, len(grouped))
	for k, dur := range grouped {
		result = append(result, storage.MediaSummary{
			Category:  k.category,
			PlayerID:  k.playerID,
			Title:     k.label,
			DurationS: dur,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].DurationS > result[j].DurationS
	})

	writeJSON(w, result)
}

func (s *Server) handleMediaTimeline(w http.ResponseWriter, r *http.Request) {
	from, to := parseRange(r)
	limit := 1000
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
			limit = l
		}
	}

	rawTimeline, err := s.db.GetRawMediaTimeline(from, to, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rCfg := s.getRules()
	timeline := make([]storage.MediaRecord, 0, len(rawTimeline))
	for _, m := range rawTimeline {
		cat, label := rCfg.CategorizeMedia(m.PlayerID, m.Title, m.URL)
		timeline = append(timeline, storage.MediaRecord{
			ID:        m.ID,
			PlayerID:  m.PlayerID,
			Category:  cat,
			Title:     label,
			StartTime: m.StartTime,
			EndTime:   m.EndTime,
			DurationS: m.DurationS,
		})
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
