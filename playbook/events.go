package playbook

import (
	"fmt"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/patrickmn/go-cache"
)

var eventPlaybooksCache = cache.New(time.Hour*1, time.Hour*1)

func eventPlaybookCacheKey(eventClass, event string) string {
	return fmt.Sprintf("%s::%s", eventClass, event)
}

func FindPlaybooksForEvent(ctx api.Context, eventClass, event string) ([]models.Playbook, error) {
	if playbooks, found := eventPlaybooksCache.Get(eventPlaybookCacheKey(eventClass, event)); found {
		return playbooks.([]models.Playbook), nil
	}

	playbooks, err := db.FindPlaybooksForEvent(ctx, eventClass, event)
	if err != nil {
		return nil, err
	}

	eventPlaybooksCache.SetDefault(eventPlaybookCacheKey(eventClass, event), playbooks)
	return playbooks, nil
}

func clearEventPlaybookCache() {
	eventPlaybooksCache.Flush()
}
