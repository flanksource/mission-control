package responder

import (
	"errors"
	"fmt"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/responder/jira"
	"github.com/flanksource/incident-commander/responder/msplanner"
	"github.com/patrickmn/go-cache"
	"gorm.io/gorm"
)

var respondersCache = cache.New(time.Hour*1, time.Hour*1)

type ResponderInterface interface {
	// NotifyResponder creates a new issue and returns the issue ID
	NotifyResponder(ctx context.Context, responder api.Responder) (string, error)
	// NotifyResponderAddComment adds a comment to an existing issue
	NotifyResponderAddComment(ctx context.Context, responder api.Responder, comment string) (string, error)
	// GetComments returns the comments for an issue
	GetComments(issueID string) ([]api.Comment, error)
	// SyncConfig gets the config for the responder for use in the UI
	SyncConfig(ctx context.Context, team api.Team) (configType string, configName string, config string, err error)
}

func GetResponder(ctx context.Context, team api.Team) (ResponderInterface, error) {
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

func FindResponderByID(ctx context.Context, id string) (*models.Responder, error) {
	if value, ok := respondersCache.Get(id); ok {
		if cache, ok := value.(*models.Responder); ok {
			return cache, nil
		}
	}

	var responder models.Responder
	if err := ctx.DB().Where("id = ?", id).Find(&responder).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}

		return nil, err
	}

	respondersCache.SetDefault(id, &responder)
	return &responder, nil
}

func PurgeCache(teamID string) {
	respondersCache.Delete(teamID)
}
