package main

import (
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"
)

type activitySample struct {
	appID string
	title string
	minS  int
	maxS  int
}

func main() {
	samples := []activitySample{
		{"foot", "nvim cmd/utrack/main.go", 300, 3600},
		{"foot", "tmux: 0:zsh* 1:build-", 120, 1800},
		{"firefox", "Rick Astley - Never Gonna Give You Up - YouTube", 200, 1500},
		{"firefox", "Lofi Hip Hop Radio - Beats to Relax/Study to - YouTube Music", 600, 5400},
		{"firefox", "how to write sway ipc client in go - Google Search", 60, 600},
		{"firefox", "tanz/utrack: Lightweight Sway time tracker - GitHub", 180, 2400},
		{"firefox", "r/unixporn - Sway rice on Arch Linux - Reddit", 120, 900},
		{"firefox", "Go slice memory allocation patterns - Stack Overflow", 90, 720},
		{"mpv", "Silicon Valley S01E01.mkv", 1200, 3600},
		{"discord", "#general - Gophers Guild", 60, 900},
		{"org.pwmt.zathura", "Operating Systems: Three Easy Pieces.pdf", 600, 3600},
		{"spotify", "Pink Floyd - Comfortably Numb", 300, 1800},
	}

	now := time.Now()
	// Target past 10 days
	startDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -9)

	var b strings.Builder
	b.WriteString("-- utrack test dummy dataset (10 days)\n")
	b.WriteString("BEGIN TRANSACTION;\n")

	r := rand.New(rand.NewSource(42))

	for dayOffset := 0; dayOffset < 10; dayOffset++ {
		currentDate := startDay.AddDate(0, 0, dayOffset)

		// Morning session: 09:30 - 13:00
		tCurrent := time.Date(currentDate.Year(), currentDate.Month(), currentDate.Day(), 9, 30+r.Intn(30), 0, 0, currentDate.Location())
		lunchTime := time.Date(currentDate.Year(), currentDate.Month(), currentDate.Day(), 13, 0, 0, 0, currentDate.Location())

		for tCurrent.Before(lunchTime) {
			s := samples[r.Intn(len(samples))]
			dur := s.minS + r.Intn(s.maxS-s.minS)
			tEnd := tCurrent.Add(time.Duration(dur) * time.Second)
			if tEnd.After(lunchTime) {
				tEnd = lunchTime
				dur = int(tEnd.Sub(tCurrent).Seconds())
			}
			if dur > 5 {
				titleEscaped := strings.ReplaceAll(s.title, "'", "''")
				b.WriteString(fmt.Sprintf("INSERT INTO activities (app_id, title, start_time, end_time, duration_s) VALUES ('%s', '%s', %d, %d, %d);\n",
					s.appID, titleEscaped, tCurrent.Unix(), tEnd.Unix(), dur))
			}
			// idle break 1-10 mins
			tCurrent = tEnd.Add(time.Duration(60+r.Intn(600)) * time.Second)
		}

		// Afternoon session: 14:15 - 18:30
		tCurrent = time.Date(currentDate.Year(), currentDate.Month(), currentDate.Day(), 14, 15+r.Intn(30), 0, 0, currentDate.Location())
		dinnerTime := time.Date(currentDate.Year(), currentDate.Month(), currentDate.Day(), 18, 30, 0, 0, currentDate.Location())

		for tCurrent.Before(dinnerTime) {
			s := samples[r.Intn(len(samples))]
			dur := s.minS + r.Intn(s.maxS-s.minS)
			tEnd := tCurrent.Add(time.Duration(dur) * time.Second)
			if tEnd.After(dinnerTime) {
				tEnd = dinnerTime
				dur = int(tEnd.Sub(tCurrent).Seconds())
			}
			if dur > 5 {
				titleEscaped := strings.ReplaceAll(s.title, "'", "''")
				b.WriteString(fmt.Sprintf("INSERT INTO activities (app_id, title, start_time, end_time, duration_s) VALUES ('%s', '%s', %d, %d, %d);\n",
					s.appID, titleEscaped, tCurrent.Unix(), tEnd.Unix(), dur))
			}
			tCurrent = tEnd.Add(time.Duration(30+r.Intn(400)) * time.Second)
		}

		// Evening session: 20:00 - 23:30 (more entertainment: youtube, mpv, reddit)
		tCurrent = time.Date(currentDate.Year(), currentDate.Month(), currentDate.Day(), 20, 0+r.Intn(30), 0, 0, currentDate.Location())
		nightTime := time.Date(currentDate.Year(), currentDate.Month(), currentDate.Day(), 23, 30, 0, 0, currentDate.Location())

		for tCurrent.Before(nightTime) {
			s := samples[r.Intn(len(samples))]
			dur := s.minS + r.Intn(s.maxS-s.minS)
			tEnd := tCurrent.Add(time.Duration(dur) * time.Second)
			if tEnd.After(nightTime) {
				tEnd = nightTime
				dur = int(tEnd.Sub(tCurrent).Seconds())
			}
			if dur > 5 {
				titleEscaped := strings.ReplaceAll(s.title, "'", "''")
				b.WriteString(fmt.Sprintf("INSERT INTO activities (app_id, title, start_time, end_time, duration_s) VALUES ('%s', '%s', %d, %d, %d);\n",
					s.appID, titleEscaped, tCurrent.Unix(), tEnd.Unix(), dur))
			}
			tCurrent = tEnd.Add(time.Duration(30+r.Intn(300)) * time.Second)
		}
	}

	b.WriteString("COMMIT;\n")

	if err := os.WriteFile("dummy_data.sql", []byte(b.String()), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write dummy_data.sql: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("dummy_data.sql generated successfully.")
}
