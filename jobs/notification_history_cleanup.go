package jobs

import (
	"github.com/flanksource/duty/job"
	"github.com/flanksource/incident-commander/db"
)

func CleanupNotificationSendHistory(ctx job.JobRuntime) error {
	count, err := db.DeleteNotificationSendHistory(ctx.Context, 30)
	ctx.History.SuccessCount = int(count)
	return err
}
