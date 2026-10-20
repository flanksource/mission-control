package playbook

import (
	"fmt"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
)

// playbookGrant is one rule of the Role bindPlaybook binds a playbook to.
type playbookGrant struct {
	Action   string
	Resource v1.ScopeTarget
	Target   *v1.ScopeTarget
}

func selfScope(pb models.Playbook) v1.ScopeTarget {
	return v1.ScopeTarget{Playbook: &v1.ScopePlaybookRef{Name: pb.Name, Namespace: pb.Namespace}}
}

var allConfigs = v1.ScopeTarget{Config: &v1.ScopeConfigSelector{Name: "*"}}

// runOnItself grants the playbook playbook:run on itself, with the given target if any.
func runOnItself(pb models.Playbook, target *v1.ScopeTarget) playbookGrant {
	return playbookGrant{Action: policy.ActionPlaybookRun, Resource: selfScope(pb), Target: target}
}

// bindPlaybook binds the playbook to a Role made of the grants, each on Scopes of its own, in the playbook's namespace.
// It returns a function that removes them.
func bindPlaybook(pb models.Playbook, grants ...playbookGrant) func() {
	ginkgo.GinkgoHelper()

	name := fmt.Sprintf("pb-%s", uuid.NewString()[:8])
	uid := func() k8sTypes.UID { return k8sTypes.UID(uuid.NewString()) }
	meta := func(n string) metav1.ObjectMeta {
		return metav1.ObjectMeta{Name: n, Namespace: pb.Namespace, UID: uid()}
	}
	persistScope := func(n string, target v1.ScopeTarget) *v1.ScopeReference {
		Expect(db.PersistScopeFromCRD(DefaultContext, &v1.Scope{
			ObjectMeta: meta(n),
			Spec:       v1.ScopeSpec{Targets: []v1.ScopeTarget{target}},
		})).To(Succeed())
		return &v1.ScopeReference{ScopeRef: n}
	}

	var rules []v1.RoleRule
	for i, grant := range grants {
		rule := v1.RoleRule{
			Name:     fmt.Sprintf("rule-%d", i),
			Action:   grant.Action,
			Resource: persistScope(fmt.Sprintf("%s-%d-resource", name, i), grant.Resource),
		}
		if grant.Target != nil {
			rule.Target = persistScope(fmt.Sprintf("%s-%d-target", name, i), *grant.Target)
		}
		rules = append(rules, rule)
	}

	Expect(db.PersistRoleFromCRD(DefaultContext, &v1.Role{ObjectMeta: meta(name), Spec: v1.RoleSpec{Rules: rules}})).To(Succeed())
	Expect(db.PersistRoleBindingFromCRD(DefaultContext, &v1.RoleBinding{
		ObjectMeta: meta(name),
		Spec: v1.RoleBindingSpec{Role: name, Subjects: v1.RoleBindingSubjects{
			PermissionGroupSubjects: v1.PermissionGroupSubjects{
				Playbooks: []v1.PermissionGroupSelector{{Name: pb.Name, Namespace: pb.Namespace}},
			},
		}},
	})).To(Succeed())
	Expect(rbac.ReloadPolicy()).To(Succeed())

	return func() {
		ginkgo.GinkgoHelper()
		Expect(DefaultContext.DB().Where("namespace = ? AND name = ?", pb.Namespace, name).Delete(&models.RoleBinding{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ? AND name = ?", pb.Namespace, name).Delete(&models.Role{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ? AND name LIKE ?", pb.Namespace, name+"-%").Delete(&models.Scope{}).Error).To(Succeed())
		Expect(rbac.ReloadPolicy()).To(Succeed())
	}
}
