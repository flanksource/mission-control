package connection

import (
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/clicky/api"

	"github.com/flanksource/incident-commander/clientapi"
)

type JWT clientapi.JWT

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

func (j JWT) ScopeCount() int {
	if j.Scopes == "" {
		return 0
	}
	return len(strings.Fields(j.Scopes))
}

func (j JWT) PrettyFull() api.Text {
	t := j.Pretty()
	if j.Raw != "" {
		t = t.NewLine().AddText("Raw: ", "font-bold").AddText(j.Raw, "text-muted")
	}
	return t
}

func DecodeJWT(token string) *JWT {
	return (*JWT)(clientapi.DecodeJWT(token))
}
