package notification_test

import (
	"github.com/casbin/casbin/v2/persist"
	stringadapter "github.com/casbin/casbin/v2/persist/string-adapter"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/tests/setup"
	notification "github.com/flanksource/incident-commander/notification"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type recoveryPolicyAdapter struct{ *stringadapter.Adapter }

func (*recoveryPolicyAdapter) AddPolicy(string, string, []string) error                  { return nil }
func (*recoveryPolicyAdapter) RemovePolicy(string, string, []string) error               { return nil }
func (*recoveryPolicyAdapter) RemoveFilteredPolicy(string, string, int, ...string) error { return nil }
func (*recoveryPolicyAdapter) AddPolicies(string, string, [][]string) error              { return nil }
func (*recoveryPolicyAdapter) RemovePolicies(string, string, [][]string) error           { return nil }

var _ = ginkgo.BeforeEach(func() {
	notification.PurgeCache("")
	if rbac.Enforcer() == nil {
		Expect(rbac.Init(setup.DefaultContext, []string{dummy.JohnDoe.ID.String()}, func(context.Context, *gormadapter.Adapter) persist.Adapter {
			return &recoveryPolicyAdapter{stringadapter.NewAdapter("# isolated notification test policies")}
		})).To(Succeed())
		rbac.Stop()
	}
	// Legacy specs previously ran without an enforcer; recovery specs exercise least-privilege policies.
	recovery, err := ginkgo.CurrentSpecReport().MatchesLabelFilter("recovery")
	Expect(err).NotTo(HaveOccurred())
	if recovery {
		var existingEvents []uuid.UUID
		Expect(setup.DefaultContext.DB().Model(&models.Event{}).Pluck("id", &existingEvents).Error).To(Succeed())
		ginkgo.DeferCleanup(func() {
			q := setup.DefaultContext.DB().Where("1 = 1")
			if len(existingEvents) > 0 {
				q = q.Where("id NOT IN ?", existingEvents)
			}
			Expect(q.Delete(&models.Event{}).Error).To(Succeed())
		})
	}
	rbac.Enforcer().EnableEnforce(recovery)
	Expect(rbac.Enforcer().InvalidateCache()).To(Succeed())
	ginkgo.DeferCleanup(func() {
		rbac.Enforcer().EnableEnforce(true)
		Expect(rbac.Enforcer().InvalidateCache()).To(Succeed())
	})
})
