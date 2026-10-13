package mcp

import (
	gocontext "context"
	"fmt"

	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
)

var _ = ginkgo.Describe("Notification tools", ginkgo.Ordered, func() {
	const namespace = "mcp-notification-tools"

	var notificationReader, silenceReader models.Person

	type notificationTool struct {
		name    string
		handler func(gocontext.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
		args    map[string]any
	}

	tools := []notificationTool{
		{toolGetNotificationDetail, getNotificationDetailHandler, map[string]any{"send_id": uuid.NewString()}},
		{toolGetNotificationsForResource, getNotificationsForResourceHandler, map[string]any{"resource_id": uuid.NewString()}},
	}

	uid := func() k8sTypes.UID { return k8sTypes.UID(uuid.NewString()) }

	// bind binds the person to a Role reading all resources of the type
	bind := func(person models.Person, resource string) {
		ginkgo.GinkgoHelper()
		name := "read-" + resource
		target := v1.ScopeTarget{}
		switch resource {
		case v1.ResourceNotification:
			target.Notification = &v1.ScopeWholeTypeSelector{Name: "*"}
		case v1.ResourceNotificationSilence:
			target.NotificationSilence = &v1.ScopeWholeTypeSelector{Name: "*"}
		default:
			ginkgo.Fail(fmt.Sprintf("unexpected resource %s", resource))
		}

		Expect(db.PersistScopeFromCRD(DefaultContext, &v1.Scope{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: uid()},
			Spec:       v1.ScopeSpec{Targets: []v1.ScopeTarget{target}},
		})).To(Succeed())
		Expect(db.PersistRoleFromCRD(DefaultContext, &v1.Role{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: uid()},
			Spec: v1.RoleSpec{Rules: []v1.RoleRule{
				{Name: "read", Action: policy.ActionRead, Resource: &v1.ScopeReference{ScopeRef: name}},
			}},
		})).To(Succeed())
		Expect(db.PersistRoleBindingFromCRD(DefaultContext, &v1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: uid()},
			Spec: v1.RoleBindingSpec{Role: name, Subjects: v1.RoleBindingSubjects{
				PermissionGroupSubjects: v1.PermissionGroupSubjects{People: []string{person.Email}},
			}},
		})).To(Succeed())
	}

	call := func(person models.Person, tool notificationTool) *mcp.CallToolResult {
		ginkgo.GinkgoHelper()
		result, err := tool.handler(DefaultContext.WithUser(&person), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: tool.name, Arguments: tool.args}})
		Expect(err).NotTo(HaveOccurred())
		return result
	}

	ginkgo.BeforeAll(func() {
		notificationReader = models.Person{ID: uuid.New(), Name: "Notification Tools Reader", Email: "notification-tools-reader@test.com"}
		silenceReader = models.Person{ID: uuid.New(), Name: "Notification Tools Silence Reader", Email: "notification-tools-silence-reader@test.com"}
		Expect(DefaultContext.DB().Create(&notificationReader).Error).To(Succeed())
		Expect(DefaultContext.DB().Create(&silenceReader).Error).To(Succeed())

		bind(notificationReader, v1.ResourceNotification)
		bind(silenceReader, v1.ResourceNotificationSilence)
		Expect(dutyRBAC.ReloadPolicy()).To(Succeed())
	})

	ginkgo.AfterAll(func() {
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.RoleBinding{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Role{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Scope{}).Error).To(Succeed())
		for _, p := range []models.Person{notificationReader, silenceReader} {
			Expect(DefaultContext.DB().Delete(&p).Error).To(Succeed())
		}
		Expect(dutyRBAC.ReloadPolicy()).To(Succeed())
	})

	for _, tool := range tools {
		ginkgo.It(tool.name+" allows a subject bound to a Role reading notifications", func() {
			result := call(notificationReader, tool)
			checkResultNotInMCPResponse(result.Content, []string{"forbidden"})
		})

		ginkgo.It(tool.name+" refuses a subject reading only notification silences", func() {
			result := call(silenceReader, tool)
			Expect(result.IsError).To(BeTrue())
			checkResultInMCPResponse(result.Content, []string{"forbidden", policy.ObjectNotification})
		})
	}
})
