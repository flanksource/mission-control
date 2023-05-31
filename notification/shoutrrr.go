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

func NewShoutrrrClient(ctx *api.Context, shoutrrrConfigs []api.NotificationConfig) (INotifier, error) {
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
	for _, service := range t.services {
		if service.config.Filter != "" {
			if valid, err := evaluateFilterExpression(service.config.Filter, responder); err != nil {
				logger.Errorf("error evaluating filter expression for service=%q: %v", service.name, err)
			} else if !valid {
				continue
			}
		}

		view := map[string]any{
			"incident": responder.Incident.AsMap(),
		}
		var buff bytes.Buffer
		if err := service.template.Execute(&buff, view); err != nil {
			logger.Errorf("error executing template for service=%q: %v", service.name, err)
		}

		var params *types.Params
		if service.config.Properties != nil {
			params = (*types.Params)(&service.config.Properties)
		}

		errors := service.sender.Send(buff.String(), params)
		for _, err := range errors {
			if err != nil {
				logger.Errorf("error sending message to service=%q: %v", service.name, err)
			}
		}
	}

	// TODO: Form error
	return nil
}

func evaluateFilterExpression(expression string, responder api.Responder) (bool, error) {
	prg, err := getOrCompileCELProgram(expression)
	if err != nil {
		return false, err
	}

	env := map[string]any{
		"incident": responder.Incident.AsMap(),
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
