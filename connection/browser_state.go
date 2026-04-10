package connection

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/flanksource/clicky/api"
)

// LocalStorageItem mirrors Playwright's per-origin localStorage entry.
type LocalStorageItem struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Cookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"`
	HTTPOnly bool    `json:"httpOnly"`
	Secure   bool    `json:"secure"`
	SameSite string  `json:"sameSite"`
}

type Cookies []Cookie

func (c Cookies) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("name").Label("Name").Build(),
		api.Column("domain").Label("Domain").Build(),
		api.Column("path").Label("Path").Build(),
		api.Column("expires").Label("Expires").Build(),
		api.Column("flags").Label("Flags").Build(),
	}
}

func (c Cookie) Row() map[string]any {
	row := map[string]any{
		"name":   c.Name,
		"domain": c.Domain,
		"path":   c.Path,
	}

	if c.Expires > 0 {
		expTime := time.Unix(int64(c.Expires), 0)
		remaining := time.Until(expTime).Round(time.Second)
		style := "text-green-600"
		if remaining < 0 {
			style = "text-red-600"
		}
		row["expires"] = api.Text{}.AddText(remaining.String(), style)
	} else {
		row["expires"] = api.Text{}.AddText("session", "text-muted")
	}

	var flags []string
	if c.Secure {
		flags = append(flags, "Secure")
	}
	if c.HTTPOnly {
		flags = append(flags, "HttpOnly")
	}
	if c.SameSite != "" {
		flags = append(flags, c.SameSite)
	}
	row["flags"] = strings.Join(flags, " ")
	return row
}

func (c Cookie) RowDetail() api.Textable {
	return api.Text{}.AddText("Value: ", "font-bold").AddText(c.Value, "text-muted")
}

func (c Cookies) Pretty() api.Text {
	t := api.Text{}.AddText(fmt.Sprintf("%d cookies", len(c)), "font-bold")
	domains := make(map[string]int)
	for _, cookie := range c {
		domains[cookie.Domain]++
	}
	for d, n := range domains {
		t = t.AddText(fmt.Sprintf("  %s(%d)", d, n), "text-muted")
	}
	for _, cookie := range c {
		t = t.NewLine().AddText("  "+cookie.Name, "font-bold").
			AddText("="+truncateStr(cookie.Value, 20), "text-muted").
			AddText(" ("+cookie.Domain+")", "text-muted")
	}
	return t
}

func (c Cookies) PrettyFull() api.Text {
	t := api.Text{}.AddText(fmt.Sprintf("%d cookies", len(c)), "font-bold")
	for _, cookie := range c {
		t = t.NewLine().AddText("  "+cookie.Name, "font-bold").
			AddText("="+cookie.Value, "").
			AddText(fmt.Sprintf(" (domain=%s path=%s)", cookie.Domain, cookie.Path), "text-muted")
	}
	return t
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

type SessionOrigin struct {
	Origin       string             `json:"origin"`
	LocalStorage []LocalStorageItem `json:"localStorage,omitempty"`
}

type PlaywrightSessionState struct {
	Cookies Cookies         `json:"cookies" pretty:"table"`
	Origins []SessionOrigin `json:"origins,omitempty"`
	Tokens  []JWT           `json:"tokens,omitempty"`
}

func (p PlaywrightSessionState) Pretty() api.Text {
	t := api.Text{}
	if len(p.Cookies) > 0 {
		t = t.Add(p.Cookies.Pretty())
	}
	if len(p.Origins) > 0 {
		if len(p.Cookies) > 0 {
			t = t.NewLine()
		}
		t = t.AddText(fmt.Sprintf("%d origins", len(p.Origins)), "font-bold")
		for _, o := range p.Origins {
			t = t.NewLine().AddText("  "+o.Origin, "font-bold").
				AddText(fmt.Sprintf(" (%d localStorage items)", len(o.LocalStorage)), "text-muted")
		}
	}
	if len(p.Tokens) > 0 {
		if len(p.Cookies) > 0 || len(p.Origins) > 0 {
			t = t.NewLine()
		}
		t = t.AddText(fmt.Sprintf("%d tokens", len(p.Tokens)), "font-bold")
		for _, tok := range p.Tokens {
			t = t.NewLine().Add(tok.Pretty())
		}
	}
	return t
}

func (p PlaywrightSessionState) PrettyFull() api.Text {
	t := api.Text{}
	if len(p.Cookies) > 0 {
		t = t.Add(p.Cookies.PrettyFull())
	}
	if len(p.Origins) > 0 {
		if len(p.Cookies) > 0 {
			t = t.NewLine()
		}
		t = t.AddText(fmt.Sprintf("%d origins", len(p.Origins)), "font-bold")
		for _, o := range p.Origins {
			t = t.NewLine().AddText("  "+o.Origin, "font-bold").
				AddText(fmt.Sprintf(" (%d items)", len(o.LocalStorage)), "text-muted")
			for _, item := range o.LocalStorage {
				t = t.NewLine().AddText("    "+item.Name, "font-bold").
					AddText("="+truncateStr(item.Value, 80), "text-muted")
			}
		}
	}
	if len(p.Tokens) > 0 {
		if len(p.Cookies) > 0 || len(p.Origins) > 0 {
			t = t.NewLine()
		}
		t = t.AddText(fmt.Sprintf("%d tokens", len(p.Tokens)), "font-bold")
		for _, tok := range p.Tokens {
			t = t.NewLine().Add(tok.PrettyFull())
		}
	}
	return t
}

func NewPlaywrightSessionState(cookies Cookies, sessionStorage map[string]string, origins []SessionOrigin, connURL string) PlaywrightSessionState {
	state := PlaywrightSessionState{
		Cookies: cookies,
		Origins: origins,
	}

	var tokens []JWT
	for key, value := range sessionStorage {
		if !strings.Contains(key, "accesstoken") && !strings.Contains(key, "idtoken") {
			continue
		}
		secret := ExtractSecret(value)
		if secret == "" {
			continue
		}
		if jwt := DecodeJWT(secret); jwt != nil {
			tokens = append(tokens, *jwt)
		}
	}
	state.Tokens = tokens

	if connURL != "" {
		if u, err := url.Parse(connURL); err == nil && u.Host != "" {
			connOrigin := fmt.Sprintf("%s://%s", u.Scheme, u.Host)
			has := false
			for _, o := range state.Origins {
				if o.Origin == connOrigin {
					has = true
					break
				}
			}
			if !has {
				state.Origins = append(state.Origins, SessionOrigin{Origin: connOrigin})
			}
		}
	}

	return state
}

func ExtractSecret(jsonValue string) string {
	var entry map[string]any
	if err := json.Unmarshal([]byte(jsonValue), &entry); err != nil {
		return ""
	}
	if secret, ok := entry["secret"].(string); ok {
		return secret
	}
	return ""
}
