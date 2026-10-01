package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestHelpMessageGivesExamplesWithinTelegramLimit(t *testing.T) {
	for _, example := range []string{"makan siang 50rb", "gaji 8 juta hari ini", "pengeluaran bulan ini", "cari transaksi", "koreksi transaksi"} {
		if !strings.Contains(helpMessage, example) {
			t.Errorf("help is missing the example %q", example)
		}
	}
	if utf8.RuneCountInString(helpMessage) > 4096 {
		t.Fatal("help exceeds Telegram's message limit")
	}
	if !strings.Contains(conversationalAgentPrompt, "/help") {
		t.Error("the agent prompt must tell the model to point declines at /help")
	}
}

func TestSetCommandsRegistersTheHelpMenu(t *testing.T) {
	var path string
	var body map[string][]map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	bot := &Bot{token: "test-token", http: server.Client(), base: server.URL}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := bot.SetCommands(ctx); err != nil {
		t.Fatal(err)
	}
	if path != "/bottest-token/setMyCommands" {
		t.Fatalf("unexpected path %q", path)
	}
	if len(body["commands"]) != 1 || body["commands"][0]["command"] != "help" {
		t.Fatalf("unexpected commands %+v", body)
	}
	if err := (&Bot{http: server.Client(), base: server.URL}).SetCommands(ctx); err == nil {
		t.Fatal("an unconfigured bot must not call Telegram")
	}
}

func TestSetCommandsRejectsOkFalse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"description":"nope"}`))
	}))
	defer server.Close()
	bot := &Bot{token: "test-token", http: server.Client(), base: server.URL}
	if err := bot.SetCommands(context.Background()); err == nil {
		t.Fatal("a 200 response with ok:false must be an error")
	}
}

func TestIsHelpCommandMatchesTypedSlashCommands(t *testing.T) {
	for _, text := range []string{"/help", "/HELP", "/start", "/help@RichmodBot", "  /start  ", "/start abc123 xyz", "/help tolong"} {
		if !isHelpCommand(text) {
			t.Errorf("%q must be a help command", text)
		}
	}
	for _, text := range []string{"", "help", "/helpme", "/starting", "makan siang /help", "/hel"} {
		if isHelpCommand(text) {
			t.Errorf("%q must not be a help command", text)
		}
	}
}
