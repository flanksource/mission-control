package cmd

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty"
	"go.opentelemetry.io/otel"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/postq/pg"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/shutdown"
	"github.com/flanksource/kopper"

	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/events"
	"github.com/flanksource/incident-commander/incidents/responder"
	"github.com/flanksource/incident-commander/jobs"
	"github.com/flanksource/incident-commander/notification"
	"github.com/flanksource/incident-commander/teams"
	"github.com/flanksource/incident-commander/vars"

	// register event handlers
	_ "github.com/flanksource/incident-commander/artifacts"
	_ "github.com/flanksource/incident-commander/catalog"
	_ "github.com/flanksource/incident-commander/connection"
	_ "github.com/flanksource/incident-commander/playbook"
	_ "github.com/flanksource/incident-commander/snapshot"
	_ "github.com/flanksource/incident-commander/upstream"
)

func launchKopper(ctx context.Context) {
	mgr, err := kopper.Manager(&kopper.ManagerOptions{
		AddToSchemeFunc: v1.AddToScheme,
	})
	if err != nil {
		shutdown.ShutdownAndExit(1, fmt.Sprintf("error creating manager: %v", err))
	}

	if _, err = kopper.SetupReconciler(ctx, mgr,
		db.PersistConnectionFromCRD,
		db.DeleteConnection,
		"connection.mission-control.flanksource.com",
	); err != nil {
		shutdown.ShutdownAndExit(1, fmt.Sprintf("Unable to create controller for Connection: %v", err))
	}

	if _, err = kopper.SetupReconciler(ctx, mgr,
		db.PersistIncidentRuleFromCRD,
		db.DeleteIncidentRule,
		"incidentrule.mission-control.flanksource.com",
	); err != nil {
		shutdown.ShutdownAndExit(1, fmt.Sprintf("Unable to create controller for IncidentRule: %v", err))
	}

	if _, err = kopper.SetupReconciler(ctx, mgr,
		db.PersistPlaybookFromCRD,
		db.DeletePlaybook,
		"playbook.mission-control.flanksource.com",
	); err != nil {
		shutdown.ShutdownAndExit(1, fmt.Sprintf("Unable to create controller for Playbook: %v", err))
	}

	if v1.NotificationReconciler, err = kopper.SetupReconciler(ctx, mgr,
		db.PersistNotificationFromCRD,
		db.DeleteNotification,
		"notification.mission-control.flanksource.com",
	); err != nil {
		shutdown.ShutdownAndExit(1, fmt.Sprintf("Unable to create controller for Notification: %v", err))
	}

	if _, err = kopper.SetupReconciler(ctx, mgr,
		notification.PersistNotificationSilenceFromCRD,
		db.DeleteNotificationSilence,
		"notificationsilence.mission-control.flanksource.com",
	); err != nil {
		shutdown.ShutdownAndExit(1, fmt.Sprintf("Unable to create controller for Notification Silence: %v", err))
	}

	if _, err := kopper.SetupReconciler(ctx, mgr,
		db.PersistPermissionFromCRD,
		db.DeletePermission,
		"permission.mission-control.flanksource.com",
	); err != nil {
		shutdown.ShutdownAndExit(1, fmt.Sprintf("Unable to create controller for Permission: %v", err))
	}

	if _, err := kopper.SetupReconciler(ctx, mgr,
		db.PersistPermissionGroupFromCRD,
		db.DeletePermissionGroup,
		"permissiongroup.mission-control.flanksource.com",
	); err != nil {
		shutdown.ShutdownAndExit(1, fmt.Sprintf("Unable to create controller for PermissionGroup: %v", err))
	}

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		shutdown.ShutdownAndExit(1, fmt.Sprintf("error running controller manager: %v", err))
	}
}

var Serve = &cobra.Command{
	Use:    "serve",
	PreRun: PreRun,
	Run: func(cmd *cobra.Command, args []string) {
		var dutyArgs []duty.StartOption
		if vars.AuthMode == auth.Kratos {
			dutyArgs = append(dutyArgs, duty.KratosAuth)
		}

		{
			// Create a dummy context to access the properties
			if context.NewContext(cmd.Context()).Properties().On(false, vars.FlagRLSEnable) {
				dutyArgs = append(dutyArgs, duty.EnableRLS)
			}
		}

		ctx, stop, err := duty.Start("mission-control", dutyArgs...)
		if err != nil {
			shutdown.ShutdownAndExit(1, fmt.Sprintf("error setting up db connection: %v", err))
		}

		e := echo.New(ctx)

		shutdown.AddHookWithPriority("echo", shutdown.PriorityIngress, func() {
			echo.Shutdown(e)
		})

		shutdown.AddHookWithPriority("database", shutdown.PriorityCritical, stop)

		shutdown.WaitForSignal()

		ctx.WithTracer(otel.GetTracerProvider().Tracer("mission-control"))
		ctx = ctx.WithNamespace(api.Namespace)

		go jobs.Start(ctx)

		events.StartConsumers(ctx)

		go tableUpdatesHandler(ctx)

		if !disableKubernetes && !disableOperators {
			go launchKopper(ctx)
		}

		listenAddr := fmt.Sprintf(":%d", httpPort)
		logger.Infof("Listening on %s", listenAddr)
		if err := e.Start(listenAddr); err != nil && !errors.Is(err, http.ErrServerClosed) {
			shutdown.ShutdownAndExit(1, fmt.Sprintf("failed to start server: %v", err))
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
	playbooksUpdateChan := notifyRouter.GetOrCreateChannel("playbooks")
	playbooksActionUpdateChan := notifyRouter.GetOrCreateChannel("playbook_run_actions")
	permissionUpdateChan := notifyRouter.GetOrCreateChannel("permissions")
	permissionGroupUpdateChan := notifyRouter.GetOrCreateChannel("permission_groups")
	teamMembersUpdateChan := notifyRouter.GetOrCreateChannel("team_members")

	// use a single job instance to maintain retention
	pushPlaybookActionsJob := jobs.PushPlaybookActions(ctx)
	pushPlaybookActionsJob.Schedule = "" // to disable jitter

	for {
		select {
		case v := <-notificationUpdateCh:
			_, id := tableActivityPayload(v)
			notification.PurgeCache(id)

		case v := <-playbooksUpdateChan:
			_, id := tableActivityPayload(v)
			query.InvalidateCacheByID[models.Playbook](id)

		case <-playbooksActionUpdateChan:
			if api.UpstreamConf.Valid() {
				pushPlaybookActionsJob.Run()
			}

		case v := <-teamsUpdateChan:
			tgOperation, id := tableActivityPayload(v)

			if tgOperation != TGOPInsert {
				responder.PurgeCache(id)
				teams.PurgeCache(id)
			}

			if tgOperation == TGOPDelete {
				if ok, err := rbac.DeleteRole(id); err != nil {
					ctx.Errorf("failed to delete rbac policy for team(%s): %v", id, err)
				} else if ok {
					if err := rbac.ReloadPolicy(); err != nil {
						ctx.Errorf("failed to reload rbac policy: %v", err)
					}
				}
			}

		case v := <-teamMembersUpdateChan:
			tgOperation, payload := tableActivityPayload(v)
			fields := strings.Fields(payload)
			if len(fields) != 2 {
				ctx.Errorf("bad payload for team_members update: %s. expected (team_id person_id)", payload)
				continue
			}
			teamID, personID := fields[0], fields[1]

			switch tgOperation {
			case TGOPDelete:
				if err := rbac.DeleteRoleForUser(personID, teamID); err != nil {
					ctx.Errorf("failed to delete team(%s)->user(%s) rbac policy: %v", teamID, personID, err)
				} else if err := rbac.ReloadPolicy(); err != nil {
					ctx.Errorf("failed to reload rbac policy: %v", err)
				}

			case TGOPInsert, TGOPUpdate:
				if err := rbac.AddRoleForUser(personID, teamID); err != nil {
					ctx.Errorf("failed to add team(%s)->user(%s) rbac policy: %v", teamID, personID, err)
				} else if err := rbac.ReloadPolicy(); err != nil {
					ctx.Errorf("failed to reload rbac policy: %v", err)
				}
			}

		case <-permissionUpdateChan:
			if err := rbac.ReloadPolicy(); err != nil {
				ctx.Logger.Errorf("error reloading rbac policy due to permission updates: %v", err)
			} else {
				ctx.Logger.Debugf("reloading rbac policy due to permission updates")
			}

			// permissions affect RLS so we need to invalidate the postgrest JWT
			// TODO: only invalidate tokens for the affect users
			auth.FlushTokenCache()

		case <-permissionGroupUpdateChan:
			if err := rbac.ReloadPolicy(); err != nil {
				ctx.Logger.Errorf("error reloading rbac policy due to permission updates: %v", err)
			} else {
				ctx.Logger.Debugf("reloading rbac policy due to permission updates")
			}

			// permissions affect RLS so we need to invalidate the postgrest JWT
			// TODO: only invalidate tokens for the affect users
			auth.FlushTokenCache()
		}
	}
}

func tableActivityPayload(payload string) (TGOP, string) {
	fields := strings.Fields(payload)
	derivedPayload := strings.Join(fields[1:], " ")
	return TGOP(fields[0]), derivedPayload
}

// TG_OP from SQL trigger functions
type TGOP string

const (
	TGOPDelete TGOP = "DELETE"
	TGOPInsert TGOP = "INSERT"
	TGOPUpdate TGOP = "UPDATE"
)
