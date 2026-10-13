package adapter

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/flanksource/duty/types"
	v1 "github.com/flanksource/incident-commander/api/v1"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Scope selectors", func() {
	for _, kind := range []string{"config", "component", "check", "canary", "playbook", "view", "connection", "scraper"} {
		for field, value := range map[string]string{
			"health": `"unhealthy"`, "statuses": `["failed"]`, "fieldSelector": `"health=unhealthy"`,
			"scope": `"all"`, "search": `"type=Pod"`, "cache": `"1m"`, "limit": `10`, "includeDeleted": `true`,
		} {
			ginkgo.It("rejects "+kind+"."+field+" even alongside a valid condition", func() {
				decoder := json.NewDecoder(strings.NewReader(fmt.Sprintf(`{"%s":{"name":"*","%s":%s}}`, kind, field, value)))
				decoder.DisallowUnknownFields()
				var target v1.ScopeTarget
				Expect(decoder.Decode(&target)).To(MatchError(ContainSubstring(`unknown field "` + field + `"`)))
			})
		}
	}

	for _, raw := range []string{
		`{"global":{"name":"*"}}`,
		`{"component":{"name":"*","tagSelector":"team=payments"}}`,
		`{"playbook":{"name":"*","labelSelector":"team=payments"}}`,
		`{"canary":{"name":"*","types":["http"]}}`,
		`{"connection":{"name":"*","agent":"cluster-a"}}`,
		`{"scraper":{"name":"*","types":["Kubernetes"]}}`,
		`{"scraper":{"name":"*","tagSelector":"team=payments"}}`,
		`{"scraper":{"name":"*","labelSelector":"team=payments"}}`,
	} {
		ginkgo.It("rejects unsupported fields in "+raw, func() {
			decoder := json.NewDecoder(strings.NewReader(raw))
			decoder.DisallowUnknownFields()
			var target v1.ScopeTarget
			Expect(decoder.Decode(&target)).To(MatchError(ContainSubstring("unknown field")))
		})
	}

	for _, tt := range []struct {
		kind     string
		fields   string
		expected types.ResourceSelector
	}{
		{"config", `"agent":"cluster-a","types":["Pod","Deployment"],"tagSelector":"team=payments","labelSelector":"app=api"`, types.ResourceSelector{Agent: "cluster-a", Types: []string{"Pod", "Deployment"}, TagSelector: "team=payments", LabelSelector: "app=api"}},
		{"component", `"agent":"cluster-b","types":["Service"],"labelSelector":"app=worker"`, types.ResourceSelector{Agent: "cluster-b", Types: []string{"Service"}, LabelSelector: "app=worker"}},
		{"check", `"agent":"cluster-c","types":["http"],"labelSelector":"app=frontend"`, types.ResourceSelector{Agent: "cluster-c", Types: []string{"http"}, LabelSelector: "app=frontend"}},
		{"canary", `"agent":"cluster-d","labelSelector":"team=ops"`, types.ResourceSelector{Agent: "cluster-d", LabelSelector: "team=ops"}},
		{"playbook", "", types.ResourceSelector{}},
		{"view", "", types.ResourceSelector{}},
		{"connection", `"types":["AWS"]`, types.ResourceSelector{Types: []string{"AWS"}}},
		{"scraper", `"agent":"cluster-e"`, types.ResourceSelector{Agent: "cluster-e"}},
	} {
		ginkgo.It("converts every supported "+tt.kind+" field without query options", func() {
			fields := `"id":"3c3e0b2a-9f1e-4d2b-8c5a-1e2f3a4b5c6d","name":"api","namespace":"production"`
			if tt.fields != "" {
				fields += "," + tt.fields
			}
			var target v1.ScopeTarget
			Expect(json.Unmarshal([]byte(fmt.Sprintf(`{"%s":{%s}}`, tt.kind, fields)), &target)).To(Succeed())
			Expect(ValidateScopeTargets([]v1.ScopeTarget{target})).To(Succeed())
			kind, selector := target.Selector()
			expected := tt.expected
			expected.ID = "3c3e0b2a-9f1e-4d2b-8c5a-1e2f3a4b5c6d"
			expected.Name = "api"
			expected.Namespace = "production"
			Expect(kind).To(Equal(tt.kind))
			Expect(selector).To(Equal(expected))
		})
	}
})

var _ = ginkgo.Describe("Whole-type Scope targets", func() {
	for _, kind := range v1.WholeTypeResources {
		decode := func(raw string) (v1.ScopeTarget, error) {
			decoder := json.NewDecoder(strings.NewReader(raw))
			decoder.DisallowUnknownFields()
			var target v1.ScopeTarget
			return target, decoder.Decode(&target)
		}

		ginkgo.It(`accepts `+kind+` with name: "*"`, func() {
			target, err := decode(fmt.Sprintf(`{"%s":{"name":"*"}}`, kind))
			Expect(err).ToNot(HaveOccurred())
			Expect(ValidateScopeTargets([]v1.ScopeTarget{target})).To(Succeed())
			Expect(MembershipTargets([]v1.ScopeTarget{target})).To(BeEmpty())
		})

		ginkgo.It("rejects "+kind+" with any other name, or none", func() {
			for _, selector := range []string{`{"name":"api"}`, `{"name":"api-*"}`, `{"name":""}`, `{}`} {
				target, err := decode(fmt.Sprintf(`{"%s":%s}`, kind, selector))
				Expect(err).ToNot(HaveOccurred())
				Expect(ValidateScopeTargets([]v1.ScopeTarget{target})).ToNot(Succeed(), selector)
			}
		})

		for _, field := range []string{`"id":"3c3e0b2a-9f1e-4d2b-8c5a-1e2f3a4b5c6d"`, `"namespace":"default"`, `"agent":"homelab"`, `"types":["x"]`, `"labelSelector":"a=b"`, `"tagSelector":"a=b"`} {
			ginkgo.It("rejects "+kind+" with "+field, func() {
				_, err := decode(fmt.Sprintf(`{"%s":{"name":"*",%s}}`, kind, field))
				Expect(err).To(MatchError(ContainSubstring("unknown field")))
			})
		}

		ginkgo.It("rejects "+kind+" alongside another type", func() {
			target, err := decode(fmt.Sprintf(`{"%s":{"name":"*"},"playbook":{"name":"*"}}`, kind))
			Expect(err).ToNot(HaveOccurred())
			Expect(ValidateScopeTargets([]v1.ScopeTarget{target})).To(MatchError(ContainSubstring("exactly one resource type")))
		})
	}
})
