package cmd

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty"
	"go.opentelemetry.io/otel"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/postq/pg"
	"github.com/flanksource/duty/shutdown"
	"github.com/flanksource/kopper"

	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/events"
	"github.com/flanksource/incident-commander/incidents/responder"
	"github.com/flanksource/incident-commander/jobs"
	"github.com/flanksource/incident-commander/notification"
	"github.com/flanksource/incident-commander/teams"

	// register event handlers
	_ "github.com/flanksource/incident-commander/artifacts"
	_ "github.com/flanksource/incident-commander/catalog"
	_ "github.com/flanksource/incident-commander/connection"
	_ "github.com/flanksource/incident-commander/notification"
	_ "github.com/flanksource/incident-commander/playbook"
	_ "github.com/flanksource/incident-commander/snapshot"
	_ "github.com/flanksource/incident-commander/upstream"
)

func launchKopper(ctx context.Context) {
	mgr, err := kopper.Manager(&kopper.ManagerOptions{
		AddToSchemeFunc: v1.AddToScheme,
	})
	if err != nil {
		logger.Fatalf("error creating manager: %v", err)
	}

	if err = kopper.SetupReconciler(ctx, mgr,
		db.PersistConnectionFromCRD,
		db.DeleteConnection,
		"connection.mission-control.flanksource.com",
	); err != nil {
		logger.Fatalf("Unable to create controller for Connection: %v", err)
	}

	if err = kopper.SetupReconciler(ctx, mgr,
		db.PersistIncidentRuleFromCRD,
		db.DeleteIncidentRule,
		"incidentrule.mission-control.flanksource.com",
	); err != nil {
		logger.Fatalf("Unable to create controller for IncidentRule: %v", err)
	}

	if err = kopper.SetupReconciler(ctx, mgr,
		db.PersistPlaybookFromCRD,
		db.DeletePlaybook,
		"playbook.mission-control.flanksource.com",
	); err != nil {
		logger.Fatalf("Unable to create controller for Playbook: %v", err)
	}

	if err = kopper.SetupReconciler(ctx, mgr,
		db.PersistNotificationFromCRD,
		db.DeleteNotification,
		"notification.mission-control.flanksource.com",
	); err != nil {
		logger.Fatalf("Unable to create controller for Notification: %v", err)
	}
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Fatalf("error running manager: %v", err)
	}
}

var Serve = &cobra.Command{
	Use:    "serve",
	PreRun: PreRun,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, stop, err := duty.Start("mission-control")
		if err != nil {
			logger.Fatalf(err.Error())
		}

		e := echo.New(ctx)

		shutdown.AddHook(func() {
			echo.Shutdown(e)
		})

		shutdown.AddHook(stop)

		shutdown.WaitForSignal()

		ctx.WithTracer(otel.GetTracerProvider().Tracer("mission-control"))
		ctx = ctx.
			WithNamespace(api.Namespace)

		go jobs.Start(ctx)

		events.StartConsumers(ctx)

		go tableUpdatesHandler(ctx)

		if !disableKubernetes && !disableOperators {
			go launchKopper(ctx)
		}

		listenAddr := fmt.Sprintf(":%d", httpPort)
		logger.Infof("Listening on %s", listenAddr)
		if err := e.Start(listenAddr); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatalf("Failed to start server: %v", err)
		}
	},
}

func init() {
	ServerFlags(Serve.Flags())
}

// tableUpdatesHandler handles all "table_activity" pg notifications.
func tableUpdatesHandler(ctx context.Context) {
	notifyRouter := pg.NewNotifyRouter()
	go notifyRouter.Run(ctx, "table_activity")

	notificationUpdateCh := notifyRouter.GetOrCreateChannel("notifications")
	teamsUpdateChan := notifyRouter.GetOrCreateChannel("teams")

	for {
		select {
		case id := <-notificationUpdateCh:
			notification.PurgeCache(id)

		case id := <-teamsUpdateChan:
			responder.PurgeCache(id)
			teams.PurgeCache(id)
		}
	}
}
