package mpris

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"utrack/internal/rules"
	"utrack/internal/storage"
)

const (
	mprisPrefix       = "org.mpris.MediaPlayer2."
	propertiesChanged = "org.freedesktop.DBus.Properties.PropertiesChanged"
	nameOwnerChanged  = "org.freedesktop.DBus.NameOwnerChanged"
)

// activeStream holds state for an active playback stream.
type activeStream struct {
	player    string
	category  string
	label     string
	startTime time.Time
}

// Listener monitors MPRIS D-Bus interfaces and records audio playback intervals.
type Listener struct {
	mu     sync.Mutex
	db     *storage.DB
	rules  *rules.Config
	conn   *dbus.Conn
	active map[string]*activeStream // keyed by player bus name
}

// New creates a new MPRIS D-Bus listener.
func New(db *storage.DB, r *rules.Config) *Listener {
	return &Listener{
		db:     db,
		rules:  r,
		active: make(map[string]*activeStream),
	}
}

// Start connects to the session bus, queries existing players, and streams signals.
func (l *Listener) Start(ctx context.Context) error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return fmt.Errorf("connect session bus: %w", err)
	}
	l.conn = conn

	// Add match rules for properties changed on MPRIS players
	call := conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0,
		"type='signal',interface='org.freedesktop.DBus.Properties',member='PropertiesChanged',path='/org/mpris/MediaPlayer2'")
	if call.Err != nil {
		return fmt.Errorf("add match properties: %w", call.Err)
	}

	// Add match rule for players appearing/disappearing
	call = conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0,
		"type='signal',interface='org.freedesktop.DBus',member='NameOwnerChanged'")
	if call.Err != nil {
		return fmt.Errorf("add match name owner: %w", call.Err)
	}

	// Discover existing players currently running
	l.scanExistingPlayers()

	signals := make(chan *dbus.Signal, 64)
	conn.Signal(signals)

	for {
		select {
		case <-ctx.Done():
			l.Flush(time.Now())
			_ = conn.Close()
			return ctx.Err()
		case sig, ok := <-signals:
			if !ok {
				return nil
			}
			l.handleSignal(sig)
		}
	}
}

// Flush closes all active media intervals and writes them to database.
func (l *Listener) Flush(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for busName, stream := range l.active {
		if stream != nil && !stream.startTime.IsZero() && now.After(stream.startTime) {
			if err := l.db.InsertMedia(stream.player, stream.category, stream.label, stream.startTime, now); err != nil {
				log.Printf("[mpris] error flushing media interval: %v", err)
			}
		}
		delete(l.active, busName)
	}
}

// Heartbeat writes elapsed time for continuously playing streams to prevent data loss.
func (l *Listener) Heartbeat(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, stream := range l.active {
		if stream != nil && !stream.startTime.IsZero() && now.After(stream.startTime) {
			if err := l.db.InsertMedia(stream.player, stream.category, stream.label, stream.startTime, now); err != nil {
				log.Printf("[mpris] error on heartbeat media flush: %v", err)
			}
			stream.startTime = now
		}
	}
}

func (l *Listener) scanExistingPlayers() {
	var names []string
	err := l.conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names)
	if err != nil {
		log.Printf("[mpris] list names error: %v", err)
		return
	}

	for _, name := range names {
		if strings.HasPrefix(name, mprisPrefix) {
			l.checkPlayerState(name)
		}
	}
}

func (l *Listener) handleSignal(sig *dbus.Signal) {
	now := time.Now()

	switch sig.Name {
	case nameOwnerChanged:
		// Args: name, old_owner, new_owner
		if len(sig.Body) >= 3 {
			name, ok1 := sig.Body[0].(string)
			newOwner, ok2 := sig.Body[2].(string)
			if ok1 && ok2 && strings.HasPrefix(name, mprisPrefix) {
				if newOwner == "" {
					// Player exited: stop active session
					l.stopPlayer(name, now)
				} else {
					// Player started: inspect status
					l.checkPlayerState(name)
				}
			}
		}

	case propertiesChanged:
		if strings.HasPrefix(sig.Sender, ":") {
			// Sender is unique connection name (e.g. ":1.123"). Find matching mpris bus name or inspect sender directly.
			l.handlePropertiesChanged(sig.Sender, sig.Body, now)
		}
	}
}

func (l *Listener) handlePropertiesChanged(sender string, body []stringOrInterface, now time.Time) {
	if len(body) < 2 {
		return
	}
	changed, ok := body[1].(map[string]dbus.Variant)
	if !ok {
		return
	}

	_, hasStatus := changed["PlaybackStatus"]
	_, hasMeta := changed["Metadata"]

	if !hasStatus && !hasMeta {
		return
	}

	l.checkPlayerState(sender)
}

type stringOrInterface = any

func (l *Listener) checkPlayerState(busName string) {
	now := time.Now()
	obj := l.conn.Object(busName, "/org/mpris/MediaPlayer2")

	statusVal, err := obj.GetProperty("org.mpris.MediaPlayer2.Player.PlaybackStatus")
	if err != nil {
		// Player might have dropped off or is unresponsive
		return
	}

	status, _ := statusVal.Value().(string)
	if status != "Playing" {
		l.stopPlayer(busName, now)
		return
	}

	// Playback is active: extract metadata
	title := ""
	url := ""

	metaVal, err := obj.GetProperty("org.mpris.MediaPlayer2.Player.Metadata")
	if err == nil {
		if metaMap, ok := metaVal.Value().(map[string]any); ok {
			if t, ok := metaMap["xesam:title"].(string); ok {
				title = t
			}
			if u, ok := metaMap["xesam:url"].(string); ok {
				url = u
			}
		}
	}

	cleanPlayer := cleanPlayerName(busName)
	cat, label := l.rules.CategorizeMedia(cleanPlayer, title, url)

	l.mu.Lock()
	defer l.mu.Unlock()

	cur := l.active[busName]
	if cur != nil && cur.label == label && cur.category == cat {
		// Same track/stream continues
		return
	}

	// Flush previous track if running
	if cur != nil && !cur.startTime.IsZero() && now.After(cur.startTime) {
		_ = l.db.InsertMedia(cur.player, cur.category, cur.label, cur.startTime, now)
	}

	// Begin new playback interval
	l.active[busName] = &activeStream{
		player:    cleanPlayer,
		category:  cat,
		label:     label,
		startTime: now,
	}
}

func (l *Listener) stopPlayer(busName string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	cur := l.active[busName]
	if cur != nil {
		if !cur.startTime.IsZero() && now.After(cur.startTime) {
			_ = l.db.InsertMedia(cur.player, cur.category, cur.label, cur.startTime, now)
		}
		delete(l.active, busName)
	}
}

// cleanPlayerName strips "org.mpris.MediaPlayer2." and suffixes like ".instance_*"
func cleanPlayerName(busName string) string {
	s := strings.TrimPrefix(busName, mprisPrefix)
	// Remove instance suffixes like "firefox.instance_1_42"
	if idx := strings.Index(s, ".instance"); idx != -1 {
		s = s[:idx]
	}
	// Remove colon if it's a unique bus name like ":1.234"
	s = strings.TrimPrefix(s, ":")
	if s == "" {
		return "unknown"
	}
	return strings.ToLower(s)
}
