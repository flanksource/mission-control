package senders

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/flanksource/duty/models"
)

type Telegram struct{}

func (t *Telegram) Send(ctx context.Context, conn *models.Connection, data Data) error {
	token := conn.Password
	chats := conn.Username
	if token == "" || chats == "" {
		return fmt.Errorf("telegram connection requires token (password) and chats (username)")
	}

	for _, chatID := range strings.Split(chats, ",") {
		chatID = strings.TrimSpace(chatID)
		if chatID == "" {
			continue
		}
		if err := telegramSendMessage(ctx, token, chatID, data); err != nil {
			return fmt.Errorf("telegram chat %s: %w", chatID, err)
		}
	}
	return nil
}

func telegramSendMessage(ctx context.Context, token, chatID string, data Data) error {
	escapedMessage := escapeMarkdownV2(data.Message)
	text := escapedMessage
	if data.Title != "" {
		text = fmt.Sprintf("*%s*\n\n%s", escapeMarkdownV2(data.Title), escapedMessage)
	}

	payload := map[string]any{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "MarkdownV2",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func escapeMarkdownV2(s string) string {
	for _, c := range []string{"_", "*", "[", "]", "(", ")", "~", "`", ">", "#", "+", "-", "=", "|", "{", "}", ".", "!"} {
		s = strings.ReplaceAll(s, c, "\\"+c)
	}
	return s
}
