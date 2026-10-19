package harness

import (
	"fmt"
	"net/http"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Selector is a Scope target. It's given the namespace of the case it's granted in.
type Selector func(namespace string) map[string]any

// Rule is a rule of the Role a case grants: an action on the resources one Selector picks, and optionally on the
// targets another picks.
type Rule struct {
	Action   string
	Resource Selector
	Target   Selector
}

// Allow is a rule allowing the action on the resources the selector picks.
func Allow(action string, resource Selector) Rule {
	return Rule{Action: action, Resource: resource}
}

// On limits the rule to the targets the selector picks.
func (r Rule) On(target Selector) Rule {
	r.Target = target
	return r
}

// object is a Scope, Role or RoleBinding written through the RBAC API.
type object struct {
	kind      string
	namespace string
	name      string
}

func (o object) path() string {
	return fmt.Sprintf("/rbac/%s/%s/%s", o.kind, o.namespace, o.name)
}

// grant is what a set of rules was written as: its Scopes, its Role and, when it's bound to someone, its RoleBinding.
type grant struct {
	role     object
	scopes   []object
	bindings []object
}

func meta(namespace, name string) map[string]any {
	return map[string]any{"name": name, "namespace": namespace}
}

// create writes an object through the RBAC API, as the admin.
func create(kind, namespace, name string, spec map[string]any) object {
	ginkgo.GinkgoHelper()
	resp := env.server.Admin.Do(Request{Method: http.MethodPost, Path: "/rbac/" + kind, Body: map[string]any{
		"metadata": meta(namespace, name), "spec": spec,
	}})
	Expect(resp.Status).To(Equal(http.StatusCreated), "creating %s %s/%s: %s", kind, namespace, name, resp)

	var created struct {
		Error *string `json:"error"`
	}
	Expect(resp.Decode(&created)).To(Succeed())
	Expect(created.Error).To(BeNil(), "%s %s/%s isn't in effect: %s", kind, namespace, name, resp)
	return object{kind: kind, namespace: namespace, name: name}
}

// remove deletes an object through the RBAC API, as the admin.
func remove(o object) {
	ginkgo.GinkgoHelper()
	resp := env.server.Admin.Do(Request{Method: http.MethodDelete, Path: o.path()})
	Expect(resp.Status).To(Equal(http.StatusOK), "deleting %s: %s", o.path(), resp)
}

// writeRole writes the rules as a Role of the name in the namespace, with a Scope for each selector.
func writeRole(namespace, name string, rules []Rule) grant {
	ginkgo.GinkgoHelper()
	g := grant{}

	scope := func(suffix string, selector Selector) string {
		scopeName := name + "-" + suffix
		g.scopes = append(g.scopes, create("scopes", namespace, scopeName, map[string]any{
			"targets": []any{selector(namespace)},
		}))
		return scopeName
	}

	var specRules []any
	for i, rule := range rules {
		specRule := map[string]any{
			"name":     fmt.Sprintf("rule-%d", i),
			"action":   rule.Action,
			"resource": map[string]any{"scopeRef": scope(fmt.Sprintf("r%d", i), rule.Resource)},
		}
		if rule.Target != nil {
			specRule["target"] = map[string]any{"scopeRef": scope(fmt.Sprintf("t%d", i), rule.Target)}
		}
		specRules = append(specRules, specRule)
	}
	g.role = create("roles", namespace, name, map[string]any{"rules": specRules})
	return g
}

// bindPerson binds the Role to the person.
func (g *grant) bindPerson(email string) {
	ginkgo.GinkgoHelper()
	g.bindings = append(g.bindings, create("role-bindings", g.role.namespace, g.role.name, map[string]any{
		"role": g.role.name, "subjects": map[string]any{"people": []string{email}},
	}))
}

// unbind deletes the grant's RoleBindings, and leaves its Role and Scopes.
func (g *grant) unbind() {
	ginkgo.GinkgoHelper()
	for _, b := range g.bindings {
		remove(b)
	}
	g.bindings = nil
}

// delete deletes everything the grant was written as.
func (g *grant) delete() {
	ginkgo.GinkgoHelper()
	g.unbind()
	remove(g.role)
	for _, s := range g.scopes {
		remove(s)
	}
	g.scopes = nil
}

// barrier waits for Mission Control to load the authorization policy as it is now.
//
// Policy changes take effect asynchronously, a short while after they're written. Every write before a barrier is
// followed by a write that changes what the barrier's person may do. Once that change is observed, the policy that
// was loaded includes every earlier write too: it's loaded whole.
type barrier struct {
	user    *Caller
	role    object
	binding *object
}

func newBarrier(user *Caller) *barrier {
	ginkgo.GinkgoHelper()
	const namespace = "authz-barrier"
	scope := create("scopes", namespace, "roles", map[string]any{"targets": []any{map[string]any{"role": map[string]any{"name": "*"}}}})
	role := create("roles", namespace, "roles-reader", map[string]any{"rules": []any{map[string]any{
		"name": "read", "action": "read", "resource": map[string]any{"scopeRef": scope.name},
	}}})
	return &barrier{user: user, role: role}
}

// wait flips whether the barrier's person may read Roles, and waits until Mission Control answers accordingly.
func (b *barrier) wait() {
	ginkgo.GinkgoHelper()
	want := http.StatusOK
	if b.binding == nil {
		binding := create("role-bindings", b.role.namespace, "roles-reader", map[string]any{
			"role": b.role.name, "subjects": map[string]any{"people": []string{b.user.Email()}},
		})
		b.binding = &binding
	} else {
		remove(*b.binding)
		b.binding = nil
		want = http.StatusForbidden
	}

	Eventually(func() int {
		return b.user.Do(Request{Method: http.MethodGet, Path: "/rbac/roles"}).Status
	}).WithTimeout(30*time.Second).WithPolling(200*time.Millisecond).Should(Equal(want), func() string {
		return "the authorization policy wasn't reloaded. Mission Control's log ends with:\n" + env.server.LogTail(80)
	})
}
