package shoutrrr

import (
	"fmt"
	"strconv"
	"time"

	"github.com/containrrr/shoutrrr"
	"github.com/containrrr/shoutrrr/pkg/router"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/api"
	"github.com/google/cel-go/cel"
	"github.com/patrickmn/go-cache"
)

var (
	prgCache = cache.New(24*time.Hour, 1*time.Hour)
)

type shoutrrrService struct {
	name   string // name of the sevice. example: Slack, Telegram, ...
	sender *router.ServiceRouter
	config api.NotificationClientConfig
}

func NewClient(ctx *api.Context, team api.Team) (*ShoutrrrClient, error) {
	teamSpec, err := team.GetSpec()
	if err != nil {
		return nil, err
	}
	shoutrrrConfig := teamSpec.ResponderClients.NotificationClients

	services := make([]shoutrrrService, 0, len(shoutrrrConfig))
	for _, conf := range shoutrrrConfig {
		sender, err := shoutrrr.CreateSender(conf.URL)
		if err != nil {
			return nil, fmt.Errorf("failed to create a shoutrrr sender client: %w", err)
		}

		serviceName, _, err := sender.ExtractServiceName(conf.URL)
		if err != nil {
			logger.Errorf("failed to extract service name: %w", err)
		}

		services = append(services, shoutrrrService{
			sender: sender,
			config: conf,
			name:   serviceName,
		})
	}

	return &ShoutrrrClient{
		services: services,
	}, nil
}

type ShoutrrrClient struct {
	services []shoutrrrService
}

func (t *ShoutrrrClient) NotifyResponderAddComment(ctx *api.Context, responder api.Responder, comment string) (string, error) {
	msg := fmt.Sprintf("New Comment on %q\n\n%s", responder.Incident.Title, comment)

	for _, service := range t.services {
		if service.config.Filter != "" {
			if valid, err := evaluateFilterExpression(service.config.Filter, responder); err != nil {
				logger.Errorf("failed to evaluate filter expression: %v", err)
			} else if !valid {
				continue
			}
		}

		errors := service.sender.Send(msg, nil)
		if errors != nil {
			return "", fmt.Errorf("failed to send message to service=%q: %v", service.name, errors)
		}
	}

	return "", nil
}

func (t *ShoutrrrClient) NotifyResponder(ctx *api.Context, responder api.Responder) (string, error) {
	msg := fmt.Sprintf("Subscribed to new incident: %s\n\n%s", responder.Incident.Title, responder.Incident.Description)

	for _, service := range t.services {
		if service.config.Filter != "" {
			if valid, err := evaluateFilterExpression(service.config.Filter, responder); err != nil {
				logger.Errorf("failed to evaluate filter expression: %v", err)
			} else if !valid {
				continue
			}
		}

		errors := service.sender.Send(msg, nil)
		if errors != nil {
			return "", fmt.Errorf("failed to send messages: %v", errors)
		}
	}

	return "", nil
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
