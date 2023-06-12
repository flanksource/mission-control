package notification

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/containrrr/shoutrrr"
	"github.com/containrrr/shoutrrr/pkg/router"
	"github.com/containrrr/shoutrrr/pkg/types"
	cTemplate "github.com/flanksource/commons/template"
	"github.com/flanksource/incident-commander/api"
	"github.com/google/cel-go/cel"
	"github.com/patrickmn/go-cache"
)

var prgCache = cache.New(24*time.Hour, 1*time.Hour)
var ErrFatal = errors.New("fatal error")

func DetermineService(rawURL string) (string, error) {
	serviceURL, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	scheme := serviceURL.Scheme
	schemeParts := strings.Split(scheme, "+")

	if len(schemeParts) > 1 {
		scheme = schemeParts[0]
	}

	return scheme, nil
}

func NewShoutrrrClient(ctx *api.Context, notification api.Notification) (Notifier, error) {
	config := notification.Config

	if err := config.HydrateConnection(ctx); err != nil {
		return nil, fmt.Errorf("failed to hydrate connection: %w", err)
	}

	sender, err := shoutrrr.CreateSender(config.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to create a shoutrrr sender client: %w", err)
	}

	notificationTemplate, err := template.New("notification-template").Parse(config.Template)
	if err != nil {
		return nil, fmt.Errorf("error parsing template: %w", errors.Join(err, ErrFatal))
	}

	client := shoutrrrClient{
		sender:   sender,
		config:   config,
		template: notificationTemplate,
	}

	return &client, nil
}

type shoutrrrClient struct {
	sender   *router.ServiceRouter
	config   api.NotificationConfig
	template *template.Template
}

func IsValid(filter string, responder api.Responder) (bool, error) {
	if filter == "" {
		return true, nil
	}

	view := map[string]any{
		"incident": responder.Incident.AsMap(),
	}

	isValid, err := evaluateFilterExpression(filter, view)
	if err != nil {
		return false, fmt.Errorf("error evaluating filter expression: %w", err)
	}

	return isValid, nil
}

func (t *shoutrrrClient) NotifyResponderAdded(ctx *api.Context, responder api.Responder) error {
	view := map[string]any{
		"incident": responder.Incident.AsMap(),
	}

	if t.config.Filter != "" {
		if valid, err := evaluateFilterExpression(t.config.Filter, view); err != nil {
			return fmt.Errorf("error evaluating filter expression: %w", err)
		} else if !valid {
			return nil
		}
	}

	var buff bytes.Buffer
	if err := t.template.Execute(&buff, view); err != nil {
		return fmt.Errorf("error executing template: %w", err)
	}

	templater := cTemplate.StructTemplater{
		Values:         view,
		ValueFunctions: true,
		DelimSets: []cTemplate.Delims{
			{Left: "{{", Right: "}}"},
			{Left: "$(", Right: ")"},
		},
	}
	if err := templater.Walk(&t.config); err != nil {
		return fmt.Errorf("error templating properties: %w", err)
	}

	var params *types.Params
	if t.config.Properties != nil {
		params = (*types.Params)(&t.config.Properties)
	}

	sendErrors := t.sender.Send(buff.String(), params)
	for _, err := range sendErrors {
		if err != nil {
			return fmt.Errorf("error publishing notification: %w", err)
		}
	}

	return nil
}

func evaluateFilterExpression(expression string, env map[string]any) (bool, error) {
	prg, err := getOrCompileCELProgram(expression)
	if err != nil {
		return false, err
	}

	out, _, err := (*prg).Eval(env)
	if err != nil {
		return false, err
	}

	return strconv.ParseBool(fmt.Sprint(out))
}

// getOrCompileCELProgram returns a cached or compiled cel.Program for the given cel expression.
func getOrCompileCELProgram(expression string) (*cel.Program, error) {
	if prg, exists := prgCache.Get(expression); exists {
		return prg.(*cel.Program), nil
	}

	env, err := cel.NewEnv(
		cel.Variable("incident", cel.AnyType),
	)
	if err != nil {
		return nil, err
	}

	ast, iss := env.Compile(expression)
	if iss.Err() != nil {
		return nil, iss.Err()
	}

	prg, err := env.Program(ast)
	if err != nil {
		return nil, err
	}

	prgCache.SetDefault(expression, &prg)
	return &prg, nil
}
