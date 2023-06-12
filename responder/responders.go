package responder

import (
	"fmt"
	"time"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/responder/jira"
	"github.com/flanksource/incident-commander/responder/msplanner"
	"github.com/patrickmn/go-cache"
)

var respondersCache = cache.New(time.Hour*1, time.Hour*1)

type ResponderInterface interface {
	// NotifyResponder creates a new issue and returns the issue ID
	NotifyResponder(ctx *api.Context, responder api.Responder) (string, error)
	// NotifyResponderAddComment adds a comment to an existing issue
	NotifyResponderAddComment(ctx *api.Context, responder api.Responder, comment string) (string, error)
	// GetComments returns the comments for an issue
	GetComments(issueID string) ([]api.Comment, error)
	// SyncConfig gets the config for the responder for use in the UI
	SyncConfig(ctx *api.Context, team api.Team) (configClass string, configName string, config string, err error)
}

func GetResponder(ctx *api.Context, team api.Team) (ResponderInterface, error) {
	if r, found := respondersCache.Get(team.ID.String()); found {
		return r.(ResponderInterface), nil
	}

	teamSpec, err := team.GetSpec()
	if err != nil {
		return nil, err
	}
	var responder ResponderInterface
	if teamSpec.ResponderClients.Jira != nil {
		responder, err = jira.NewClient(ctx, team)
		if err != nil {
			return nil, err
		}
	} else if teamSpec.ResponderClients.MSPlanner != nil {
		responder, err = msplanner.NewClient(ctx, team)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("no responder client found for team %s", team.ID)
	}

	respondersCache.Set(team.ID.String(), responder, cache.DefaultExpiration)
	return responder, nil
}

func PurgeCache(teamID string) {
	respondersCache.Delete(teamID)
}
