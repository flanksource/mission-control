package senders

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"context"
	"github.com/flanksource/duty/models"
)

type Ntfy struct{}

func (n *Ntfy) Send(ctx context.Context, conn *models.Connection, data Data) error {
	host := conn.URL
	if host == "" {
		host = "https://ntfy.sh"
	}
	host = strings.TrimRight(host, "/")

	topic := conn.Username
	if topic == "" {
		if props := conn.Properties; props != nil {
			topic = props["topic"]
		}
	}
	if topic == "" {
		return fmt.Errorf("ntfy connection requires a topic")
	}

	req, err := http.NewRequest(http.MethodPost, host+"/"+topic, strings.NewReader(data.Message))
	if err != nil {
		return err
	}
	if data.Title != "" {
		req.Header.Set("Title", data.Title)
	}
	if conn.Password != "" {
		req.SetBasicAuth(conn.Username, conn.Password)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ntfy returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}
