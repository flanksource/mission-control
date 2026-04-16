package senders

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"context"
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
		if err := telegramSendMessage(token, chatID, data); err != nil {
			return fmt.Errorf("telegram chat %s: %w", chatID, err)
		}
	}
	return nil
}

func telegramSendMessage(token, chatID string, data Data) error {
	payload := map[string]any{
		"chat_id":    chatID,
		"text":       data.Message,
		"parse_mode": "MarkdownV2",
	}
	if data.Title != "" {
		payload["text"] = fmt.Sprintf("*%s*\n\n%s", escapeMarkdownV2(data.Title), data.Message)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := httpClient.Post(
		fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token),
		"application/json",
		bytes.NewReader(body),
	)
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
