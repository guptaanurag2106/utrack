package tracker

import (
	"log"
	"os/exec"
	"sync"
	"time"

	"utrack/internal/storage"
)

const (
	// DefaultTitleDebounce is the duration a new title must remain stable before splitting the interval.
	DefaultTitleDebounce = 5 * time.Second

	// HeartbeatInterval is the periodic flush interval for active focus.
	HeartbeatInterval = 60 * time.Second

	// MaxSleepTolerance is the maximum wall-clock jump allowed before concluding the system was sleeping/suspended.
	MaxSleepTolerance = 90 * time.Second
)

// Tracker coordinates state tracking between Sway events and SQLite storage.
type Tracker struct {
	mu sync.Mutex
	db *storage.DB

	activeAppID     string
	activeTitle     string
	activeStartTime time.Time
	lastHeartbeat   time.Time

	pendingTitle  string
	debounceTimer *time.Timer
}

// New creates a new Tracker instance.
func New(db *storage.DB) *Tracker {
	return &Tracker{
		db:            db,
		lastHeartbeat: time.Now(),
	}
}

// Heartbeat periodically writes elapsed time and detects system suspend/sleep jumps.
func (t *Tracker) Heartbeat(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	gap := now.Sub(t.lastHeartbeat)
	t.lastHeartbeat = now

	// If wall-clock jumped significantly beyond the interval (e.g. system suspended / slept),
	// clamp the active window's credit so sleep time isn't falsely attributed to the app.
	if gap > MaxSleepTolerance {
		log.Printf("[tracker] system sleep detected (gap: %v). Discarding sleep duration.", gap.Round(time.Second))
		// Flush only up to 1 heartbeat of active time prior to sleep
		if t.activeAppID != "" && !t.activeStartTime.IsZero() {
			preSleepEnd := t.activeStartTime.Add(HeartbeatInterval)
			if preSleepEnd.Before(now) {
				t.flushLocked(preSleepEnd)
			}
		}
		// Reset start time to now (post-wake)
		t.activeStartTime = now
		return
	}

	// Normal periodic incremental flush
	if t.activeAppID != "" && !t.activeStartTime.IsZero() {
		t.flushLocked(now)
	}
}

// OnFocus updates the currently focused window. It flushes the previous interval if one was active.
func (t *Tracker) OnFocus(appID, title string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Stop any pending title debounce timer
	if t.debounceTimer != nil {
		t.debounceTimer.Stop()
		t.debounceTimer = nil
		t.pendingTitle = ""
	}

	if t.activeAppID == appID && t.activeTitle == title {
		return
	}

	// Flush existing interval if active
	t.flushLocked(now)

	if appID == "" {
		t.activeAppID = ""
		t.activeTitle = ""
		t.activeStartTime = time.Time{}
		return
	}

	t.activeAppID = appID
	t.activeTitle = title
	t.activeStartTime = now
}

// OnTitleChange schedules a debounced title transition for the currently focused window.
func (t *Tracker) OnTitleChange(appID, newTitle string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if newTitle == "" || appID != t.activeAppID || newTitle == t.activeTitle {
		return
	}

	t.pendingTitle = newTitle

	if t.debounceTimer != nil {
		t.debounceTimer.Stop()
	}

	t.debounceTimer = time.AfterFunc(DefaultTitleDebounce, func() {
		t.mu.Lock()
		defer t.mu.Unlock()

		if t.pendingTitle == "" || t.pendingTitle == t.activeTitle {
			return
		}

		fireTime := time.Now()
		// Flush current active title interval
		t.flushLocked(fireTime)

		// Start new interval with pending title
		t.activeTitle = t.pendingTitle
		t.activeStartTime = fireTime
		t.pendingTitle = ""
		t.debounceTimer = nil
	})
}

// Flush closes the current active interval and writes it to database.
func (t *Tracker) Flush(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.debounceTimer != nil {
		t.debounceTimer.Stop()
		t.debounceTimer = nil
	}
	t.flushLocked(now)
}

func (t *Tracker) flushLocked(now time.Time) {
	if t.activeAppID == "" || t.activeStartTime.IsZero() {
		return
	}

	if now.After(t.activeStartTime) {
		if err := t.db.InsertActivity(t.activeAppID, t.activeTitle, t.activeStartTime, now); err != nil {
			log.Printf("[tracker] error saving activity: %v", err)
		}
	}

	t.activeStartTime = now
}

// GetCurrentState returns a snapshot of the current active window.
func (t *Tracker) GetCurrentState() (appID, title string, started time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.activeAppID, t.activeTitle, t.activeStartTime
}

// NotifyError attempts to send a desktop notification via notify-send.
func NotifyError(title, msg string) {
	_ = exec.Command("notify-send", "-u", "critical", title, msg).Run()
}
