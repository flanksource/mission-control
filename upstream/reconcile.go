package upstream

import (
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/upstream"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
)

var ReconcilePageSize int

// SyncWithUpstream coordinates with upstream and pushes any resource
// that are missing on the upstream.
func SyncWithUpstream(ctx *api.Context) error {
	jobHistory := models.NewJobHistory("SyncWithUpstream", api.UpstreamConf.Host, "")
	_ = db.PersistJobHistory(ctx, jobHistory.Start())
	defer func() {
		_ = db.PersistJobHistory(ctx, jobHistory.End())
	}()

	syncer := upstream.NewUpstreamSyncer(api.UpstreamConf, ReconcilePageSize)
	for _, table := range api.TablesToReconcile {
		if err := syncer.SyncTableWithUpstream(ctx, table); err != nil {
			jobHistory.AddError(err.Error())
			logger.Errorf("failed to sync table %s: %w", table, err)
		} else {
			jobHistory.IncrSuccess()
		}
	}

	return nil
}
