package cmd

import (
	gocontext "context"
	"fmt"
	"net/url"
	"os"
	"strings"

	commonsCtx "github.com/flanksource/commons/context"
	"github.com/flanksource/commons/logger"
	"go.opentelemetry.io/otel"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/kopper"
	"github.com/flanksource/postq/pg"

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
	_ "github.com/flanksource/incident-commander/playbook"
	_ "github.com/flanksource/incident-commander/upstream"
)

const (
	propertiesFile = "mission-control.properties"
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
		// PostgREST needs to know how it is exposed to create the correct links
		db.HttpEndpoint = api.PublicWebURL + "/db"

		ctx := context.NewContext(gocontext.Background(), commonsCtx.WithTracer(otel.GetTracerProvider().Tracer("mission-control"))).
			WithDB(db.Gorm, db.Pool).
			WithKubernetes(api.Kubernetes).
			WithNamespace(api.Namespace)

		if _, err := os.Stat(propertiesFile); err == nil {
			if err := context.LoadPropertiesFromFile(ctx, propertiesFile); err != nil {
				logger.Fatalf("Error setting properties in database: %v", err)
			}
		}

		if api.PostgrestURI != "" {
			parsedURL, err := url.Parse(api.PostgrestURI)
			if err != nil {
				logger.Fatalf("Failed to parse PostgREST URL: %v", err)
			}

			host := strings.ToLower(parsedURL.Hostname())
			if host == "localhost" {
				go db.StartPostgrest(parsedURL.Port())
			}
		}

		go jobs.Start(ctx)

		events.StartConsumers(ctx)

		go tableUpdatesHandler(ctx)

		if !disableKubernetes {
			go launchKopper(ctx)
		}

		e := echo.New(ctx)

		listenAddr := fmt.Sprintf(":%d", httpPort)
		logger.Infof("Listening on %s", listenAddr)
		if err := e.Start(listenAddr); err != nil {
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

	notificationUpdateCh := notifyRouter.RegisterRoutes("notifications")
	teamsUpdateChan := notifyRouter.RegisterRoutes("teams")

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
