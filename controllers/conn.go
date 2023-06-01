package controllers

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/flanksource/commons/logger"
	missioncontrolv1 "github.com/flanksource/incident-commander/api/v1"
	"k8s.io/apimachinery/pkg/api/errors"
)

// ConnectionReconciler reconciles a Connection object
type ConnectionReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	OnDeleteFunc func(string) error
	OnUpsertFunc func(missioncontrolv1.Connection) error
}

const ConnectionFinalizerName = "connection.mission-control.flanksource.com"

//+kubebuilder:rbac:groups=mission-control.flanksource.com,resources=connections,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mission-control.flanksource.com,resources=connections/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mission-control.flanksource.com,resources=connections/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Connection object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *ConnectionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var connectionObj missioncontrolv1.Connection
	err := r.Get(ctx, req.NamespacedName, &connectionObj)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Errorf("TODO Not found")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Check if it is deleted and then remove
	if !connectionObj.DeletionTimestamp.IsZero() {
		logger.Infof("deleting connection[%s]", connectionObj.GetUID())
		if err := r.OnDeleteFunc(string(connectionObj.GetUID())); err != nil {
			logger.Errorf("failed to delete connection: %v", err)
			return ctrl.Result{Requeue: true, RequeueAfter: 2 * time.Minute}, err
		}
		controllerutil.RemoveFinalizer(&connectionObj, ConnectionFinalizerName)
		return ctrl.Result{}, r.Update(ctx, &connectionObj)
	}

	if !controllerutil.ContainsFinalizer(&connectionObj, ConnectionFinalizerName) {
		controllerutil.AddFinalizer(&connectionObj, ConnectionFinalizerName)
		if err := r.Update(ctx, &connectionObj); err != nil {
			logger.Errorf("failed to update finalizers %v", err)
			return ctrl.Result{Requeue: true, RequeueAfter: 2 * time.Minute}, err
		}
	}

	if err := r.OnUpsertFunc(connectionObj); err != nil {
		logger.Errorf("failed to upsert connection: %v", err)
		return ctrl.Result{Requeue: true, RequeueAfter: 2 * time.Minute}, err
	}

	logger.Infof("upserted connection[%s]", connectionObj.GetUID())
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ConnectionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&missioncontrolv1.Connection{}).
		Complete(r)
}
