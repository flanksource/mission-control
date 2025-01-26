package db

import (
	"time"

	"github.com/flanksource/duty/context"
	"github.com/patrickmn/go-cache"
)

var distinctTagsCache = cache.New(time.Minute*10, time.Hour)

func GetDistinctTags(ctx context.Context) ([]string, error) {
	if cached, ok := distinctTagsCache.Get("key"); ok {
		return cached.([]string), nil
	}

	var tags []string
	query := `
	SELECT jsonb_object_keys(tags) FROM config_items
	UNION
	SELECT jsonb_object_keys(tags) FROM playbooks`
	if err := ctx.DB().Raw(query).Scan(&tags).Error; err != nil {
		return nil, err
	}

	distinctTagsCache.SetDefault("key", tags)
	return tags, nil
}
