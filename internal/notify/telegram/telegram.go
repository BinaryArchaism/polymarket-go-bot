package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

type Notifier struct {
	botToken string
	chatID   int64
	enabled  bool
	client   *http.Client
	logger   zerolog.Logger

	mu    sync.Mutex
	queue []string
}

type sendMessageRequest struct {
	ChatID    int64  `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
}

type telegramResponse struct {
	Ok          bool   `json:"ok"`
	Description string `json:"description,omitempty"`
}

func NewNotifier(botToken string, chatID int64, enabled bool, logger zerolog.Logger) *Notifier {
	return &Notifier{
		botToken: botToken,
		chatID:   chatID,
		enabled:  enabled,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger.With().Str("component", "telegram").Logger(),
		queue:  make([]string, 0),
	}
}

func (n *Notifier) SendMessage(ctx context.Context, message string) error {
	if !n.enabled {
		return nil
	}

	if n.botToken == "" || n.chatID == 0 {
		return fmt.Errorf("telegram bot token or chat ID not configured")
	}

	req := sendMessageRequest{
		ChatID: n.chatID,
		Text:   message,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", n.botToken)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var tgResp telegramResponse
	if err := json.Unmarshal(respBody, &tgResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if !tgResp.Ok {
		return fmt.Errorf("telegram api error: %s", tgResp.Description)
	}

	return nil
}

func (n *Notifier) SendError(ctx context.Context, component string, err error) {
	message := fmt.Sprintf("🔴 ERROR [%s]\n%s", component, err.Error())
	if sendErr := n.SendMessage(ctx, message); sendErr != nil {
		n.logger.Warn().Err(sendErr).Msg("failed to send error to telegram")
	}
}

func (n *Notifier) SendInfo(ctx context.Context, message string) {
	msg := fmt.Sprintf("ℹ️ %s", message)
	if err := n.SendMessage(ctx, msg); err != nil {
		n.logger.Warn().Err(err).Msg("failed to send info to telegram")
	}
}

func (n *Notifier) SendWarning(ctx context.Context, message string) {
	msg := fmt.Sprintf("⚠️ WARNING\n%s", message)
	if err := n.SendMessage(ctx, msg); err != nil {
		n.logger.Warn().Err(err).Msg("failed to send warning to telegram")
	}
}

func (n *Notifier) SendSuccess(ctx context.Context, message string) {
	msg := fmt.Sprintf("✅ %s", message)
	if err := n.SendMessage(ctx, msg); err != nil {
		n.logger.Warn().Err(err).Msg("failed to send success to telegram")
	}
}
