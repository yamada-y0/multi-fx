package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func runNote(args []string) {
	fs := flag.NewFlagSet("note", flag.ExitOnError)
	stateDir := fs.String("state-dir", "", "エージェントディレクトリパス（必須）")
	fs.Parse(args)

	if *stateDir == "" || len(fs.Args()) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: fxd note --state-dir <dir> <メッセージ>")
		os.Exit(1)
	}

	jst := time.FixedZone("JST", 9*60*60)
	ts := time.Now().In(jst).Format("2006-01-02 15:04:05 JST")
	line := fmt.Sprintf("- [%s] %s\n", ts, strings.Join(fs.Args(), " "))

	notesPath := filepath.Join(*stateDir, "notes.md")
	f, err := os.OpenFile(notesPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("open notes: %v", err)
	}
	defer f.Close()

	if _, err := f.WriteString(line); err != nil {
		log.Fatalf("write note: %v", err)
	}

	printJSON(map[string]string{"noted": strings.Join(fs.Args(), " ")})
}

func runNotify(args []string) {
	fs := flag.NewFlagSet("notify", flag.ExitOnError)
	fs.Parse(args)

	if len(fs.Args()) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: fxd notify <メッセージ>")
		os.Exit(1)
	}

	token := os.Getenv("LINE_CHANNEL_ACCESS_TOKEN")
	userID := os.Getenv("LINE_USER_ID")
	if token == "" || userID == "" {
		log.Fatalf("LINE_CHANNEL_ACCESS_TOKEN / LINE_USER_ID が未設定")
	}

	msg := strings.Join(fs.Args(), " ")
	body, _ := json.Marshal(map[string]any{
		"to": userID,
		"messages": []map[string]string{
			{"type": "text", "text": msg},
		},
	})

	req, err := http.NewRequest("POST", "https://api.line.me/v2/bot/message/push", bytes.NewReader(body))
	if err != nil {
		log.Fatalf("notify: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("notify: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		log.Fatalf("notify: status %d: %s", resp.StatusCode, b)
	}

	printJSON(map[string]string{"notified": msg})
}
