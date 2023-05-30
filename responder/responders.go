package responder

import (
	"fmt"
	"time"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/responder/jira"
	"github.com/flanksource/incident-commander/responder/msplanner"
	"github.com/flanksource/incident-commander/responder/shoutrrr"
	"github.com/patrickmn/go-cache"
)

var (
	respondersCache = cache.New(time.Hour*1, time.Hour*1)
)

type IBidirectionalResponder interface {
	INotifierResponder

	// GetComments returns the comments for an issue
	GetComments(issueID string) ([]api.Comment, error)
	// SyncConfig gets the config for the responder for use in the UI
	SyncConfig(ctx *api.Context, team api.Team) (configClass string, configName string, config string, err error)
}

type INotifierResponder interface {
	// NotifyResponder creates a new issue and returns the issue ID
	NotifyResponder(ctx *api.Context, responder api.Responder) (string, error)
	// NotifyResponderAddComment adds a comment to an existing issue
	NotifyResponderAddComment(ctx *api.Context, responder api.Responder, comment string) (string, error)
}

func getResponderClient(ctx *api.Context, team api.Team, isNotifier bool) (any, error) {
	if r, found := respondersCache.Get(team.ID.String()); found {
		return r, nil
	}

	teamSpec, err := team.GetSpec()
	if err != nil {
		return nil, fmt.Errorf("failed to get team spec: %w", err)
	}

	var responder any
	if teamSpec.ResponderClients.Jira != nil {
		responder, err = jira.NewClient(ctx, team)
	} else if teamSpec.ResponderClients.MSPlanner != nil {
		responder, err = msplanner.NewClient(ctx, team)
	} else if isNotifier && len(teamSpec.ResponderClients.NotificationClients) != 0 {
		responder, err = shoutrrr.NewClient(ctx, team)
	}

	if err != nil {
		return nil, err
	}

	if responder == nil {
		return nil, fmt.Errorf("no responder client found for team %s", team.ID)
	}

	respondersCache.Set(team.ID.String(), responder, cache.DefaultExpiration)
	return responder, nil
}

func GetBidirectionalResponder(ctx *api.Context, team api.Team) (IBidirectionalResponder, error) {
	responder, err := getResponderClient(ctx, team, false)
	if err != nil {
		return nil, err
	}

	if bidirectionalResponder, ok := responder.(IBidirectionalResponder); ok {
		return bidirectionalResponder, nil
	}

	return nil, fmt.Errorf("no responder client found for team %s", team.ID)
}

func GetNotifierResponder(ctx *api.Context, team api.Team) (INotifierResponder, error) {
	responder, err := getResponderClient(ctx, team, true)
	if err != nil {
		return nil, err
	}

	return responder.(INotifierResponder), nil
}
