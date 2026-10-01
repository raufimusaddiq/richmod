package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// helpMessage is the one answer to /help, /start, and the model's finance_help
// tool, so a household sees the same examples wherever they ask.
const helpMessage = "Richmod membantu mencatat keuangan keluarga.\n\n" +
	"Contoh pesan:\n" +
	"• makan siang 50rb\n" +
	"• gaji 8 juta hari ini\n" +
	"• pengeluaran bulan ini\n" +
	"• cari transaksi Alfamart\n" +
	"• koreksi transaksi Alfamart ke kemarin\n\n" +
	"Kirim foto struk, slip gaji, atau bukti transfer untuk dicatat otomatis. " +
	"Kalau ada yang belum jelas, Richmod bertanya lewat tombol atau meminta kamu membalas pesannya."

// isHelpCommand reports whether a typed message is /help or /start, with an
// optional @BotName suffix and trailing text. Typed text reaches the worker
// through the agent lane, so this check must run before any model call.
func isHelpCommand(text string) bool {
	fields := strings.Fields(strings.ToLower(text))
	if len(fields) == 0 {
		return false
	}
	command, _, _ := strings.Cut(fields[0], "@")
	return command == "/help" || command == "/start"
}

// botCommands is the menu Telegram shows next to the message box.
func botCommands() []map[string]string {
	return []map[string]string{{"command": "help", "description": "Contoh pesan dan cara memakai Richmod"}}
}

// SetCommands registers the command menu. It is best-effort at worker start;
// every finance flow works without it.
func (b *Bot) SetCommands(ctx context.Context) error {
	if b.token == "" {
		return fmt.Errorf("Telegram commands are not configured")
	}
	body, err := json.Marshal(map[string]any{"commands": botCommands()})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, b.base+"/bot"+b.token+"/setMyCommands", bytes.NewReader(body))
	if err != nil {
		// The request URL contains the bot token; never wrap its errors.
		return fmt.Errorf("create Telegram commands request")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := b.http.Do(request)
	if err != nil {
		// The transport error stringifies the URL, token included.
		return fmt.Errorf("set Telegram commands failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Telegram commands API returned HTTP %d", response.StatusCode)
	}
	var result struct {
		OK bool `json:"ok"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result) != nil || !result.OK {
		return fmt.Errorf("Telegram commands API returned an invalid response")
	}
	return nil
}
