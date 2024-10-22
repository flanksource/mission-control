package auth

import (
	"fmt"
	"html/template"

	"github.com/flanksource/duty/shutdown"
)

var inviteUserTemplate *template.Template

func init() {
	parsed, err := template.New("email").Parse(`
<b>Welcome to Mission Control</b>
<br><br>
Hello {{.firstName}},<br>
Please visit <a href="{{.link}}">{{.link}}</a> to complete registration and use the code: <code>{{.code}}</code>`)
	if err != nil {
		shutdown.ShutdownAndExit(1, fmt.Sprintf("failed to parse invitation email template: %v", err))
	}

	inviteUserTemplate = parsed
}
