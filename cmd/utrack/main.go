package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"utrack/internal/ipc"
	"utrack/internal/lock"
	"utrack/internal/storage"
	"utrack/internal/tracker"
	"utrack/internal/web"
)

func getLockPath() string {
	if rt := os.Getenv("XDG_RUNTIME_DIR"); rt != "" {
		testPath := filepath.Join(rt, "utrack.lock")
		if f, err := os.OpenFile(testPath, os.O_CREATE|os.O_RDWR, 0600); err == nil {
			_ = f.Close()
			return testPath
		}
	}
	dbPath, _ := storage.GetDefaultDBPath()
	return filepath.Join(filepath.Dir(dbPath), "utrack.lock")
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "daemon":
		runDaemon()
	case "web":
		runWeb(os.Args[2:])
	case "status":
		runStatus()
	case "clear":
		runClear(os.Args[2:])
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`utrack — Sway Arch Linux Activity Tracker

Usage:
  utrack <command> [options]

Commands:
  daemon          Run the background activity tracking daemon
  web [--port P]  Launch the local dashboard web interface (default port: 8080)
  status          Show current tracker status and today's summary
  clear [--force] Clear all stored activity logs from the database
  help            Show this help text`)
}

func runDaemon() {
	lockPath := getLockPath()
	fileLock, err := lock.Acquire(lockPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		_ = fileLock.Release()
	}()

	dbPath, err := storage.GetDefaultDBPath()
	if err != nil {
		log.Fatalf("[daemon] resolve db path: %v", err)
	}

	db, err := storage.Open(dbPath)
	if err != nil {
		log.Fatalf("[daemon] open storage at %s: %v", dbPath, err)
	}
	defer db.Close()

	tr := tracker.New(db)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)

	go func() {
		sig := <-sigChan
		log.Printf("[daemon] caught signal %s, flushing active session...", sig)
		tr.Flush(time.Now())
		_ = db.Close()
		_ = fileLock.Release()
		os.Exit(0)
	}()

	log.Printf("[daemon] utrack daemon started (db: %s, pid: %d)", dbPath, os.Getpid())

	// Periodic heartbeat ticker (flushes every minute and detects suspend/sleep)
	ticker := time.NewTicker(tracker.HeartbeatInterval)
	defer ticker.Stop()
	go func() {
		for range ticker.C {
			tr.Heartbeat(time.Now())
		}
	}()

	// Daemon event processing loop with auto-reconnect
	for {
		err := runListener(tr)
		if err != nil {
			log.Printf("[daemon] sway connection lost: %v. Retrying in 2s...", err)
			tr.Flush(time.Now())
			time.Sleep(2 * time.Second)
		}
	}
}

func runListener(tr *tracker.Tracker) error {
	client, err := ipc.Connect()
	if err != nil {
		return err
	}
	defer client.Close()

	// Initial tree lookup to set the active window right away
	if focused, err := client.GetFocusedWindow(); err == nil && focused != nil {
		tr.OnFocus(focused.AppName(), focused.Name, time.Now())
		log.Printf("[daemon] initial focus: app=%q title=%q", focused.AppName(), focused.Name)
	}

	if err := client.SubscribeEvents(); err != nil {
		return fmt.Errorf("subscribe to window and workspace events: %w", err)
	}

	for {
		msgType, payload, err := client.ReadMessage()
		if err != nil {
			return fmt.Errorf("read ipc frame: %w", err)
		}

		now := time.Now()

		switch msgType {
		case ipc.EventWindowMask:
			var event ipc.WindowEvent
			if err := json.Unmarshal(payload, &event); err != nil {
				continue
			}
			switch event.Change {
			case "focus":
				tr.OnFocus(event.Container.AppName(), event.Container.Name, now)
			case "title":
				tr.OnTitleChange(event.Container.AppName(), event.Container.Name, now)
			case "close":
				// If a window closed, verify if active window still exists
				if focused, err := client.GetFocusedWindow(); err == nil && focused != nil {
					tr.OnFocus(focused.AppName(), focused.Name, now)
				} else {
					tr.OnFocus("", "", now)
				}
			}

		case ipc.EventWorkspaceMask:
			var event ipc.WorkspaceEvent
			if err := json.Unmarshal(payload, &event); err != nil {
				continue
			}
			if event.Change == "focus" {
				// Empty workspace check: does the workspace contain any focused window container?
				if event.Current != nil {
					if focused := ipc.FindFocused(event.Current); focused != nil {
						tr.OnFocus(focused.AppName(), focused.Name, now)
					} else {
						// Empty workspace selected
						tr.OnFocus("", "", now)
					}
				}
			}
		}
	}
}

func runWeb(args []string) {
	fs := flag.NewFlagSet("web", flag.ExitOnError)
	port := fs.Int("port", 8080, "HTTP dashboard port")
	_ = fs.Parse(args)

	dbPath, err := storage.GetDefaultDBPath()
	if err != nil {
		log.Fatalf("[web] resolve db path: %v", err)
	}

	db, err := storage.Open(dbPath)
	if err != nil {
		log.Fatalf("[web] open storage: %v", err)
	}
	defer db.Close()

	srv := web.NewServer(db, *port)
	if err := srv.Start(); err != nil {
		log.Fatalf("[web] server failed: %v", err)
	}
}

func runStatus() {
	lockPath := getLockPath()
	fl, err := lock.Acquire(lockPath)
	isRunning := false
	if err != nil {
		isRunning = true
	} else {
		_ = fl.Release()
	}

	statusStr := "STOPPED"
	if isRunning {
		statusStr = "RUNNING"
	}

	dbPath, err := storage.GetDefaultDBPath()
	if err != nil {
		fmt.Printf("Daemon Status: %s\nError locating database: %v\n", statusStr, err)
		return
	}

	db, err := storage.Open(dbPath)
	if err != nil {
		fmt.Printf("Daemon Status: %s\nDatabase: %s (unreachable)\n", statusStr, dbPath)
		return
	}
	defer db.Close()

	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
	endOfDay := startOfDay + 86400

	summaries, err := db.GetSummary(startOfDay, endOfDay)
	var totalSecs int64
	for _, s := range summaries {
		totalSecs += s.DurationS
	}

	fmt.Printf("Daemon Status:    %s\n", statusStr)
	fmt.Printf("Database Path:    %s\n", dbPath)
	fmt.Printf("Today's Time:     %s\n", formatDuration(totalSecs))
	fmt.Printf("Tracked Apps:     %d\n", len(summaries))
	if len(summaries) > 0 {
		fmt.Printf("Top Application:  %s (%s)\n", summaries[0].AppID, formatDuration(summaries[0].DurationS))
	}
}

func formatDuration(s int64) string {
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	m := s / 60
	s = s % 60
	if m < 60 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	h := m / 60
	m = m % 60
	return fmt.Sprintf("%dh %dm", h, m)
}

func runClear(args []string) {
	fs := flag.NewFlagSet("clear", flag.ExitOnError)
	force := fs.Bool("force", false, "Bypass confirmation prompt")
	fs.BoolVar(force, "f", false, "Bypass confirmation prompt (shorthand)")
	_ = fs.Parse(args)

	if !*force {
		fmt.Print("Warning: This will permanently delete all activity logs. Proceed? [y/N]: ")
		var resp string
		_, _ = fmt.Scanln(&resp)
		if resp != "y" && resp != "Y" && resp != "yes" {
			fmt.Println("Aborted.")
			return
		}
	}

	dbPath, err := storage.GetDefaultDBPath()
	if err != nil {
		log.Fatalf("[clear] resolve db path: %v", err)
	}

	db, err := storage.Open(dbPath)
	if err != nil {
		log.Fatalf("[clear] open db: %v", err)
	}
	defer db.Close()

	if err := db.TruncateActivities(); err != nil {
		log.Fatalf("[clear] failed: %v", err)
	}
	fmt.Println("Activity history cleared.")
}
