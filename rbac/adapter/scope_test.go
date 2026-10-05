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
	for _, kind := range []string{"config", "component", "check", "canary", "playbook", "view", "connection"} {
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
