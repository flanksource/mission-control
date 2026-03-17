package jobs

import (
	"strings"

	"github.com/flanksource/duty/job"

	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
)

var cleanupExpiredTokens = &job.Job{
	Name:       "CleanupExpiredTokens",
	Schedule:   "@every 12h",
	Singleton:  true,
	JobHistory: true,
	Retention:  job.RetentionFew,
	RunNow:     false,
	Fn: func(ctx job.JobRuntime) error {
		expiredTokens, err := db.GetExpiredAccessTokens(ctx.Context)
		if err != nil {
			return ctx.Oops().Wrapf(err, "error fetching expired tokens")
		}

		var cleanedCount, failedCount int
		var failureErrors []string

		for _, token := range expiredTokens {
			if err := auth.DeleteAccessToken(ctx.Context, token.ID.String()); err != nil {
				ctx.Errorf("failed to delete expired token %s: %v", token.ID, err)
				failedCount++
				failureErrors = append(failureErrors, token.ID.String())
				continue
			}

			cleanedCount++
		}

		ctx.History.SuccessCount = cleanedCount

		if failedCount > 0 {
			return ctx.Oops().Errorf("cleaned %d tokens but failed to delete %d tokens: %s",
				cleanedCount, failedCount, strings.Join(failureErrors, ", "))
		}

		return nil
	},
}
