package notification

import (
	"bytes"
	"fmt"
	"strconv"
	"text/template"
	"time"

	"github.com/containrrr/shoutrrr"
	"github.com/containrrr/shoutrrr/pkg/router"
	"github.com/containrrr/shoutrrr/pkg/types"
	"github.com/flanksource/commons/logger"
	cTemplate "github.com/flanksource/commons/template"
	"github.com/flanksource/incident-commander/api"
	"github.com/google/cel-go/cel"
	"github.com/patrickmn/go-cache"
)

var (
	prgCache = cache.New(24*time.Hour, 1*time.Hour)
)

type shoutrrrService struct {
	name     string // name of the sevice. example: Slack, Telegram, ...
	sender   *router.ServiceRouter
	config   api.NotificationConfig
	template *template.Template
}

func NewShoutrrrClient(ctx *api.Context, shoutrrrConfigs []api.NotificationConfig) (Notifier, error) {
	services := make([]shoutrrrService, 0, len(shoutrrrConfigs))
	for _, config := range shoutrrrConfigs {
		if err := config.HydrateConnection(ctx); err != nil {
			logger.Errorf("failed to hydrate connection: %v", err)
			continue
		}

		sender, err := shoutrrr.CreateSender(config.URL)
		if err != nil {
			logger.Errorf("failed to create a shoutrrr sender client: %v", err)
			continue
		}

		notificationTemplate, err := template.New("notification-template").Parse(config.Template)
		if err != nil {
			logger.Errorf("error parsing template: %v", err)
			continue
		}

		serviceName, _, err := sender.ExtractServiceName(config.URL)
		if err != nil {
			logger.Errorf("failed to extract service name: %w", err)
		}

		services = append(services, shoutrrrService{
			sender:   sender,
			config:   config,
			template: notificationTemplate,
			name:     serviceName,
		})
	}

	return &shoutrrrClient{services: services}, nil
}

type shoutrrrClient struct {
	services []shoutrrrService
}

func (t *shoutrrrClient) NotifyResponderAdded(ctx *api.Context, responder api.Responder) error {
	var errCollection []error
	for _, service := range t.services {
		view := map[string]any{
			"incident": responder.Incident.AsMap(),
		}

		if service.config.Filter != "" {
			if valid, err := evaluateFilterExpression(service.config.Filter, view); err != nil {
				logger.Errorf("error evaluating filter expression for service=%q: %v", service.name, err)
			} else if !valid {
				continue
			}
		}

		var buff bytes.Buffer
		if err := service.template.Execute(&buff, view); err != nil {
			logger.Errorf("error executing template for service=%q: %v", service.name, err)
			continue
		}

		templater := cTemplate.StructTemplater{
			Values:         view,
			ValueFunctions: true,
			DelimSets: []cTemplate.Delims{
				{Left: "{{", Right: "}}"},
				{Left: "$(", Right: ")"},
			},
		}
		if err := templater.Walk(&service.config); err != nil {
			logger.Errorf("error templating properties: %v", err)
			continue
		}

		var params *types.Params
		if service.config.Properties != nil {
			params = (*types.Params)(&service.config.Properties)
		}

		sendErrors := service.sender.Send(buff.String(), params)
		for _, err := range sendErrors {
			if err != nil {
				logger.Errorf("error sending message to service=%q: %v", service.name, err)
			}
		}
	}

	if len(errCollection) > 0 {
		return fmt.Errorf("multiple errors encountered: %v", errCollection)
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
