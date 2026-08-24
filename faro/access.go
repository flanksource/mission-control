package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/commons/duration"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/sdk"
)

// accessConfigSearchLimit caps how many configs a positional query may resolve
// to before the access rows themselves are fetched.
const accessConfigSearchLimit = 500

// accessClient returns the remote client and a background context, the entry
// point every access subcommand shares.
func accessClient() (*sdk.Client, context.Context, error) {
	client, err := fullRemoteClient()
	if err != nil {
		return nil, nil, err
	}
	return client, context.Background(), nil
}

// resolveConfigIDs turns positional catalog query args into config ids via the
// same remote search `catalog search` uses. No args means "every config"; args
// that match nothing are an error rather than a silent full export.
func resolveConfigIDs(ctx context.Context, client *sdk.Client, args []string) ([]string, error) {
	searchQuery := strings.TrimSpace(strings.Join(args, " "))
	if searchQuery == "" {
		return nil, nil
	}

	resp, err := client.SearchCatalog(ctx, query.SearchResourcesRequest{
		Limit:   accessConfigSearchLimit,
		Configs: []types.ResourceSelector{{Search: searchQuery, Agent: "all"}},
	})
	if err != nil {
		return nil, err
	}
	if len(resp.Configs) == 0 {
		return nil, fmt.Errorf("no configs match %q", searchQuery)
	}
	if len(resp.Configs) >= accessConfigSearchLimit {
		logger.Warnf("config query %q hit the %d result cap; access rows below are incomplete", searchQuery, accessConfigSearchLimit)
	}

	ids := make([]string, 0, len(resp.Configs))
	for _, config := range resp.Configs {
		ids = append(ids, config.ID)
	}
	return ids, nil
}

// resolveUserIDs resolves an optional --user MatchItem filter to every matching user id.
func resolveUserIDs(ctx context.Context, client *sdk.Client, arg string) ([]string, error) {
	if strings.TrimSpace(arg) == "" {
		return nil, nil
	}
	users, _, err := client.ListExternalUsers(ctx, sdk.IdentityOptions{Name: arg})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(users))
	for _, user := range users {
		ids = append(ids, user.ID.String())
	}
	return ids, nil
}

// warnTruncated reports loudly when --limit cut the export short, so a partial
// export is never mistaken for a complete one.
func warnTruncated(kind string, shown, total int) {
	if total > shown {
		logger.Warnf("showing %d of %d %s; raise --limit to export them all", shown, total, kind)
	}
}

// parseSince converts a duration string ("90d", "6w", "24h") into an absolute
// lower bound on created_at.
func parseSince(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := duration.ParseDuration(value)
	if err != nil {
		return nil, fmt.Errorf("invalid --since %q: %w", value, err)
	}
	since := time.Now().Add(-time.Duration(parsed))
	return &since, nil
}
