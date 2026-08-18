package clientcmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	clickyapi "github.com/flanksource/clicky/api"

	"github.com/flanksource/incident-commander/clientapi"
)

func jwtScopeCount(token clientapi.JWT) int {
	return len(strings.Fields(token.Scopes))
}

func prettyJWT(token clientapi.JWT, full bool) clickyapi.Text {
	text := clickyapi.Text{}
	if token.Name != "" {
		text = text.AddText("Name: ", "font-bold").AddText(token.Name, "")
	}
	if token.UPN != "" {
		text = text.AddText("  UPN: ", "font-bold").AddText(token.UPN, "")
	}
	if token.Audience != "" {
		text = text.NewLine().AddText("Audience: ", "font-bold").AddText(token.Audience, "text-blue-600")
	}
	if token.AppID != "" {
		text = text.AddText("  AppID: ", "font-bold").AddText(token.AppID, "")
	}
	if token.Scopes != "" {
		text = text.NewLine().AddText("Scopes: ", "font-bold").AddText(token.Scopes, "text-muted")
	}
	if !token.ExpiresAt.IsZero() {
		remaining := time.Until(token.ExpiresAt).Round(time.Second)
		style := "text-green-600"
		if remaining < 0 {
			style = "text-red-600"
		} else if remaining < 10*time.Minute {
			style = "text-yellow-600"
		}
		text = text.NewLine().AddText("Expires: ", "font-bold").
			AddText(token.ExpiresAt.Format(time.RFC3339), "").
			AddText(fmt.Sprintf(" (%s)", remaining), style)
	}
	if full && token.Raw != "" {
		text = text.NewLine().AddText("Raw: ", "font-bold").AddText(token.Raw, "text-muted")
	}
	return text
}

func prettySessionState(state clientapi.PlaywrightSessionState, full bool) clickyapi.Text {
	text := clickyapi.Text{}
	if len(state.Cookies) > 0 {
		text = text.Add(prettyCookies(state.Cookies, full))
	}
	if len(state.Origins) > 0 {
		if len(state.Cookies) > 0 {
			text = text.NewLine()
		}
		text = text.AddText(fmt.Sprintf("%d origins", len(state.Origins)), "font-bold")
		for _, origin := range state.Origins {
			text = text.NewLine().AddText("  "+origin.Origin, "font-bold").
				AddText(fmt.Sprintf(" (%d localStorage items)", len(origin.LocalStorage)), "text-muted")
			if full {
				for _, item := range origin.LocalStorage {
					text = text.NewLine().AddText("    "+item.Name, "font-bold").
						AddText("="+truncateSessionValue(item.Value, 80), "text-muted")
				}
			}
		}
	}
	if len(state.Tokens) > 0 {
		if len(state.Cookies) > 0 || len(state.Origins) > 0 {
			text = text.NewLine()
		}
		text = text.AddText(fmt.Sprintf("%d tokens", len(state.Tokens)), "font-bold")
		for _, token := range state.Tokens {
			text = text.NewLine().Add(prettyJWT(token, full))
		}
	}
	return text
}

func prettyCookies(cookies clientapi.Cookies, full bool) clickyapi.Text {
	text := clickyapi.Text{}.AddText(fmt.Sprintf("%d cookies", len(cookies)), "font-bold")
	if full {
		for _, cookie := range cookies {
			text = text.NewLine().AddText("  "+cookie.Name, "font-bold").
				AddText("="+cookie.Value, "").
				AddText(fmt.Sprintf(" (domain=%s path=%s)", cookie.Domain, cookie.Path), "text-muted")
		}
		return text
	}
	domains := make(map[string]int)
	for _, cookie := range cookies {
		domains[cookie.Domain]++
	}
	names := make([]string, 0, len(domains))
	for domain := range domains {
		names = append(names, domain)
	}
	sort.Strings(names)
	for _, domain := range names {
		text = text.AddText(fmt.Sprintf("  %s(%d)", domain, domains[domain]), "text-muted")
	}
	for _, cookie := range cookies {
		text = text.NewLine().AddText("  "+cookie.Name, "font-bold").
			AddText("="+truncateSessionValue(cookie.Value, 20), "text-muted").
			AddText(" ("+cookie.Domain+")", "text-muted")
	}
	return text
}

func truncateSessionValue(value string, maxLength int) string {
	if len(value) <= maxLength {
		return value
	}
	return value[:maxLength] + "..."
}
