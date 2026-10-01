package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

// outOfScopeMessage is the one refusal for anything that is not household
// finance. It says what Richmod does and where to see examples.
const outOfScopeMessage = "Richmod hanya membantu pencatatan, pencarian, koreksi, arus kas, dan tinjauan keuangan keluarga. Ketik /help untuk contoh."

const unsupportedFeatureMessage = "Richmod belum mendukung fitur investasi atau perintah sistem. Ketik /help untuk contoh."

// botCommands is the menu Telegram shows next to the message box.
var botCommands = []map[string]string{
	{"command": "help", "description": "Contoh pesan dan cara memakai Richmod"},
}

// SetCommands registers the command menu. It is best-effort at worker start;
// every finance flow works without it.
func (b *Bot) SetCommands(ctx context.Context) error {
	if b.token == "" {
		return fmt.Errorf("Telegram commands are not configured")
	}
	body, err := json.Marshal(map[string]any{"commands": botCommands})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, b.base+"/bot"+b.token+"/setMyCommands", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create Telegram commands request")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := b.http.Do(request)
	if err != nil {
		return fmt.Errorf("set Telegram commands failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Telegram commands API returned HTTP %d", response.StatusCode)
	}
	return nil
}
