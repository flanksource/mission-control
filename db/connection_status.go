package db

import (
	"fmt"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	v1 "github.com/flanksource/incident-commander/api/v1"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func connectionRef(namespace, name string) string {
	if namespace == "" || name == "" {
		return ""
	}
	return fmt.Sprintf("connection://%s/%s", namespace, name)
}

func setConnectionRef(obj *v1.Connection) {
	if obj == nil {
		return
	}

	if obj.Status.Ref == "" {
		obj.Status.Ref = connectionRef(obj.Namespace, obj.Name)
	}
}

func setConnectionStatusCondition(obj *v1.Connection, status metav1.ConditionStatus, reason, message string) {
	if obj == nil {
		return
	}

	setConnectionRef(obj)
	now := metav1.Now()
	obj.Status.ObservedGeneration = obj.Generation

	k8smeta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               v1.ConditionReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: obj.Generation,
		LastTransitionTime: now,
	})
}

func setConnectionPersistFailedStatus(obj *v1.Connection, err error) {
	if err == nil {
		return
	}
	setConnectionStatusCondition(obj, metav1.ConditionFalse, v1.ReadyReasonPersistFailed, err.Error())
}

func setConnectionDeleteFailedStatus(obj *v1.Connection, err error) {
	if err == nil {
		return
	}
	setConnectionStatusCondition(obj, metav1.ConditionFalse, v1.ReadyReasonDeleteFailed, err.Error())
}

func setConnectionReadyStatus(obj *v1.Connection) {
	setConnectionStatusCondition(obj, metav1.ConditionTrue, v1.ReadyReasonSynced, "Connection is valid and persisted")
}

func persistConnectionStatus(ctx context.Context, obj *v1.Connection) {
	if obj == nil || obj.Namespace == "" || obj.Name == "" {
		return
	}

	setConnectionRef(obj)

	k8s, err := ctx.LocalKubernetes()
	if err != nil {
		ctx.Tracef("failed to initialize kubernetes client for connection status update %s/%s: %v", obj.Namespace, obj.Name, err)
		return
	}

	resourceClient, err := k8s.GetClientByGroupVersionKind(ctx, v1.GroupVersion.Group, v1.GroupVersion.Version, "Connection")
	if err != nil {
		ctx.Tracef("failed to load connection resource client for status update %s/%s: %v", obj.Namespace, obj.Name, err)
		return
	}

	resource, err := resourceClient.Namespace(obj.Namespace).Get(ctx, obj.Name, metav1.GetOptions{})
	if err != nil {
		ctx.Tracef("failed to fetch connection %s/%s while updating status: %v", obj.Namespace, obj.Name, err)
		return
	}

	if obj.Generation == 0 {
		obj.Generation = resource.GetGeneration()
	}

	var mergedStatus v1.ConnectionStatus
	existingStatusMap, found, err := unstructured.NestedMap(resource.Object, "status")
	if err != nil {
		ctx.Tracef("failed to read existing connection status for %s/%s: %v", obj.Namespace, obj.Name, err)
		return
	}

	if found {
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(existingStatusMap, &mergedStatus); err != nil {
			ctx.Tracef("failed to decode existing connection status for %s/%s: %v", obj.Namespace, obj.Name, err)
			return
		}
	}

	if obj.Status.ObservedGeneration != 0 {
		mergedStatus.ObservedGeneration = obj.Status.ObservedGeneration
	} else if obj.Generation != 0 && len(obj.Status.Conditions) > 0 {
		mergedStatus.ObservedGeneration = obj.Generation
	}

	for _, c := range obj.Status.Conditions {
		if obj.Generation != 0 && c.ObservedGeneration == 0 {
			c.ObservedGeneration = obj.Generation
		}
		k8smeta.SetStatusCondition(&mergedStatus.Conditions, c)
	}

	if mergedStatus.Ref == "" {
		mergedStatus.Ref = obj.Status.Ref
	}

	statusMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&mergedStatus)
	if err != nil {
		ctx.Tracef("failed to convert connection status for %s/%s: %v", obj.Namespace, obj.Name, err)
		return
	}

	if err := unstructured.SetNestedMap(resource.Object, statusMap, "status"); err != nil {
		ctx.Tracef("failed to set connection status for %s/%s: %v", obj.Namespace, obj.Name, err)
		return
	}

	if _, err := resourceClient.Namespace(obj.Namespace).UpdateStatus(ctx, resource, metav1.UpdateOptions{}); err != nil {
		ctx.Tracef("failed to persist connection status for %s/%s: %v", obj.Namespace, obj.Name, err)
	}
}

func persistConnectionDeleteFailedStatus(ctx context.Context, conn models.Connection, err error) {
	if err == nil || conn.Name == "" || conn.Namespace == "" {
		return
	}

	obj := &v1.Connection{
		ObjectMeta: metav1.ObjectMeta{
			Name:      conn.Name,
			Namespace: conn.Namespace,
		},
	}

	setConnectionDeleteFailedStatus(obj, err)
	persistConnectionStatus(ctx, obj)
}
