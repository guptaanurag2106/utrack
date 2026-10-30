package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// ActivityRecord represents a single tracked window interval.
type ActivityRecord struct {
	ID        int64  `json:"id"`
	AppID     string `json:"app_id"`
	Title     string `json:"title"`
	StartTime int64  `json:"start_time"` // unix timestamp (seconds)
	EndTime   int64  `json:"end_time"`   // unix timestamp (seconds)
	DurationS int64  `json:"duration_s"` // duration in seconds
}

// AppSummary represents aggregated duration per application.
type AppSummary struct {
	AppID     string `json:"app_id"`
	DurationS int64  `json:"duration_s"`
}

// TitleSummary represents aggregated duration per title within an app.
type TitleSummary struct {
	Title     string `json:"title"`
	DurationS int64  `json:"duration_s"`
}

// DailySummary represents total duration aggregated per calendar day.
type DailySummary struct {
	Date      string `json:"date"` // formatted "YYYY-MM-DD"
	DurationS int64  `json:"duration_s"`
}

// DB wraps SQLite database operations.
type DB struct {
	db *sql.DB
}

// GetDefaultDBPath returns the standard XDG path for utrack.db or falls back gracefully.
func GetDefaultDBPath() (string, error) {
	if override := os.Getenv("UTRACK_DB"); override != "" {
		return override, nil
	}

	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			dataHome = filepath.Join(home, ".local", "share")
		}
	}

	if dataHome != "" {
		dir := filepath.Join(dataHome, "utrack")
		if err := os.MkdirAll(dir, 0755); err == nil {
			return filepath.Join(dir, "utrack.db"), nil
		}
	}

	// Fallback to local directory if XDG is not writable
	return "utrack.db", nil
}

// Open initializes or connects to SQLite with WAL mode enabled.
func Open(path string) (*DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create db dir %s: %w", dir, err)
	}

	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}

	// Single writer / connection pool settings
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(5)

	if err := initSchema(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	return &DB{db: db}, nil
}

func initSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS activities (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		app_id TEXT NOT NULL,
		title TEXT NOT NULL,
		start_time INTEGER NOT NULL,
		end_time INTEGER NOT NULL,
		duration_s INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_activities_start ON activities(start_time);
	CREATE INDEX IF NOT EXISTS idx_activities_app ON activities(app_id);
	`
	_, err := db.Exec(schema)
	return err
}

// TruncateActivities deletes all tracked activity records and vacuums the database.
func (d *DB) TruncateActivities() error {
	_, err := d.db.Exec("DELETE FROM activities; VACUUM;")
	if err != nil {
		return fmt.Errorf("truncate activities: %w", err)
	}
	return nil
}

// Close closes the database connection.
func (d *DB) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

// InsertActivity records a completed activity interval.
func (d *DB) InsertActivity(appID, title string, start, end time.Time) error {
	dur := int64(end.Sub(start).Seconds())
	if dur <= 0 {
		return nil // skip 0-second micro-blips
	}

	query := `INSERT INTO activities (app_id, title, start_time, end_time, duration_s) VALUES (?, ?, ?, ?, ?)`
	_, err := d.db.Exec(query, appID, title, start.Unix(), end.Unix(), dur)
	if err != nil {
		return fmt.Errorf("insert activity (%s, %s): %w", appID, title, err)
	}
	return nil
}

// GetSummary returns total duration per app between from and to unix timestamps.
func (d *DB) GetSummary(fromUnix, toUnix int64) ([]AppSummary, error) {
	query := `
		SELECT app_id, SUM(duration_s) AS total_s
		FROM activities
		WHERE start_time >= ? AND end_time <= ?
		GROUP BY app_id
		ORDER BY total_s DESC
	`
	rows, err := d.db.Query(query, fromUnix, toUnix)
	if err != nil {
		return nil, fmt.Errorf("query summary: %w", err)
	}
	defer rows.Close()

	var list []AppSummary
	for rows.Next() {
		var s AppSummary
		if err := rows.Scan(&s.AppID, &s.DurationS); err != nil {
			return nil, err
		}
		list = append(list, s)
	}
	return list, rows.Err()
}

// GetAppTitles returns aggregated duration per title for a specific app.
func (d *DB) GetAppTitles(appID string, fromUnix, toUnix int64) ([]TitleSummary, error) {
	query := `
		SELECT title, SUM(duration_s) AS total_s
		FROM activities
		WHERE app_id = ? AND start_time >= ? AND end_time <= ?
		GROUP BY title
		ORDER BY total_s DESC
	`
	rows, err := d.db.Query(query, appID, fromUnix, toUnix)
	if err != nil {
		return nil, fmt.Errorf("query app titles: %w", err)
	}
	defer rows.Close()

	var list []TitleSummary
	for rows.Next() {
		var s TitleSummary
		if err := rows.Scan(&s.Title, &s.DurationS); err != nil {
			return nil, err
		}
		list = append(list, s)
	}
	return list, rows.Err()
}

// TimeBounds represents the earliest and latest timestamp of recorded activities.
type TimeBounds struct {
	MinTime int64 `json:"min_time"`
	MaxTime int64 `json:"max_time"`
}

// GetTimeBounds returns the earliest start_time and latest end_time in activities.
func (d *DB) GetTimeBounds() (TimeBounds, error) {
	var bounds TimeBounds
	row := d.db.QueryRow("SELECT COALESCE(MIN(start_time), 0), COALESCE(MAX(end_time), 0) FROM activities")
	err := row.Scan(&bounds.MinTime, &bounds.MaxTime)
	return bounds, err
}

// GetDailyBreakdown returns aggregated durations grouped by day for a time interval.
func (d *DB) GetDailyBreakdown(fromUnix, toUnix int64) ([]DailySummary, error) {
	query := `
		SELECT strftime('%Y-%m-%d', start_time, 'unixepoch', 'localtime') AS day, SUM(duration_s) AS total_s
		FROM activities
		WHERE start_time >= ? AND end_time <= ?
		GROUP BY day
		ORDER BY day ASC
	`
	rows, err := d.db.Query(query, fromUnix, toUnix)
	if err != nil {
		return nil, fmt.Errorf("query daily breakdown: %w", err)
	}
	defer rows.Close()

	var list []DailySummary
	for rows.Next() {
		var s DailySummary
		if err := rows.Scan(&s.Date, &s.DurationS); err != nil {
			return nil, err
		}
		list = append(list, s)
	}
	return list, rows.Err()
}

// GetTimeline returns chronological activities within the given time range.
func (d *DB) GetTimeline(fromUnix, toUnix int64, limit int) ([]ActivityRecord, error) {
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	query := `
		SELECT id, app_id, title, start_time, end_time, duration_s
		FROM activities
		WHERE start_time >= ? AND end_time <= ?
		ORDER BY start_time ASC
		LIMIT ?
	`
	rows, err := d.db.Query(query, fromUnix, toUnix, limit)
	if err != nil {
		return nil, fmt.Errorf("query timeline: %w", err)
	}
	defer rows.Close()

	var list []ActivityRecord
	for rows.Next() {
		var r ActivityRecord
		if err := rows.Scan(&r.ID, &r.AppID, &r.Title, &r.StartTime, &r.EndTime, &r.DurationS); err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, rows.Err()
}
