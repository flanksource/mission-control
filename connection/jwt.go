package connection

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/clicky/api"
)

type JWT struct {
	Audience  string    `json:"audience,omitempty"`
	Subject   string    `json:"subject,omitempty"`
	UPN       string    `json:"upn,omitempty"`
	Name      string    `json:"name,omitempty"`
	Scopes    string    `json:"scopes,omitempty"`
	AppID     string    `json:"appid,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	Raw       string    `json:"-"`
}

func (j JWT) Pretty() api.Text {
	t := api.Text{}
	if j.Name != "" {
		t = t.AddText("Name: ", "font-bold").AddText(j.Name, "")
	}
	if j.UPN != "" {
		t = t.AddText("  UPN: ", "font-bold").AddText(j.UPN, "")
	}
	if j.Audience != "" {
		t = t.NewLine().AddText("Audience: ", "font-bold").AddText(j.Audience, "text-blue-600")
	}
	if j.AppID != "" {
		t = t.AddText("  AppID: ", "font-bold").AddText(j.AppID, "")
	}
	if j.Scopes != "" {
		t = t.NewLine().AddText("Scopes: ", "font-bold").AddText(j.Scopes, "text-muted")
	}
	if !j.ExpiresAt.IsZero() {
		remaining := time.Until(j.ExpiresAt).Round(time.Second)
		style := "text-green-600"
		if remaining < 0 {
			style = "text-red-600"
		} else if remaining < 10*time.Minute {
			style = "text-yellow-600"
		}
		t = t.NewLine().AddText("Expires: ", "font-bold").
			AddText(j.ExpiresAt.Format(time.RFC3339), "").
			AddText(fmt.Sprintf(" (%s)", remaining), style)
	}
	return t
}

func (j JWT) PrettyFull() api.Text {
	t := j.Pretty()
	if j.Raw != "" {
		t = t.NewLine().AddText("Raw: ", "font-bold").AddText(j.Raw, "text-muted")
	}
	return t
}

func DecodeJWT(token string) *JWT {
	if token == "" {
		return nil
	}
	parts := strings.SplitN(token, ".", 3)
	if len(parts) < 2 {
		return nil
	}
	payload := parts[1]
	if m := len(payload) % 4; m != 0 {
		payload += strings.Repeat("=", 4-m)
	}
	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return nil
	}
	var claims map[string]any
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return nil
	}

	j := &JWT{Raw: token}
	if v, ok := claims["aud"].(string); ok {
		j.Audience = v
	}
	if v, ok := claims["sub"].(string); ok {
		j.Subject = v
	}
	if v, ok := claims["upn"].(string); ok {
		j.UPN = v
	}
	if v, ok := claims["name"].(string); ok {
		j.Name = v
	}
	if v, ok := claims["scp"].(string); ok {
		j.Scopes = v
	}
	if v, ok := claims["appid"].(string); ok {
		j.AppID = v
	}
	if exp, ok := claims["exp"].(float64); ok {
		j.ExpiresAt = time.Unix(int64(exp), 0)
	}
	return j
}
