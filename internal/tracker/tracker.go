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

	if t.detectSleepLocked(now) {
		return
	}
	t.lastHeartbeat = now

	// Normal periodic incremental flush
	if t.activeAppID != "" && !t.activeStartTime.IsZero() {
		t.flushLocked(now)
	}
}

// detectSleepLocked detects suspend gaps for heartbeat and event-triggered
// flushes. Calling it on focus/title events too prevents a wake event arriving
// before the next heartbeat from recording the suspended interval.
func (t *Tracker) detectSleepLocked(now time.Time) bool {
	// Sub prefers monotonic timestamps when both values have them. On Linux,
	// the monotonic clock can stop during suspend, so compare wall-clock
	// timestamps explicitly to detect the full suspend interval.
	gap := wallClockElapsed(t.lastHeartbeat, now)
	if gap <= MaxSleepTolerance {
		return false
	}

	log.Printf("[tracker] system sleep detected (gap: %v). Discarding time since last heartbeat.", gap.Round(time.Second))
	// Only credit time known to have elapsed before suspend: the most recent
	// heartbeat. Do not estimate another heartbeat interval, which could
	// include time after the machine actually went to sleep. Round(0) strips
	// monotonic data so storage uses this wall-clock boundary.
	if t.activeAppID != "" && !t.activeStartTime.IsZero() {
		lastKnownAwake := t.lastHeartbeat.Round(0)
		if wallClockElapsed(t.activeStartTime, lastKnownAwake) > 0 {
			t.flushLocked(lastKnownAwake)
		}
	}

	// A timer that expired during suspend must not later flush a sleep-sized
	// interval. A fresh title event after wake can schedule a new debounce.
	if t.debounceTimer != nil {
		t.debounceTimer.Stop()
		t.debounceTimer = nil
		t.pendingTitle = ""
	}

	// Restart accounting at wake; never credit the suspended interval.
	t.activeStartTime = now
	t.lastHeartbeat = now
	return true
}

func wallClockElapsed(from, to time.Time) time.Duration {
	fromWall := time.Unix(from.Unix(), int64(from.Nanosecond()))
	toWall := time.Unix(to.Unix(), int64(to.Nanosecond()))
	return toWall.Sub(fromWall)
}

// OnFocus updates the currently focused window. It flushes the previous interval if one was active.
func (t *Tracker) OnFocus(appID, title string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.detectSleepLocked(now)

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

	t.detectSleepLocked(now)

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

		fireTime := time.Now()
		if t.detectSleepLocked(fireTime) {
			return
		}

		if t.pendingTitle == "" || t.pendingTitle == t.activeTitle {
			return
		}

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

	t.detectSleepLocked(now)

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

	if wallClockElapsed(t.activeStartTime, now) > 0 {
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
