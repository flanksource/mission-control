package db

import (
	"fmt"
	"net/url"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Transform Query to postgREST", ginkgo.Ordered, func() {
	now := time.Now()

	testData := []struct {
		description string
		input       url.Values
		output      url.Values
	}{
		{
			description: "positive alternatives are case-insensitive OR terms",
			input: url.Values{
				"change_type":        []string{"eq=diff"},
				"config_type.filter": []string{"Kubernetes::Pod,kubernetes::Deployment"},
			},
			output: url.Values{
				"change_type": []string{"eq=diff"},
				"and":         []string{"(or=(config_type.ilike.Kubernetes::Pod,config_type.ilike.kubernetes::Deployment))"},
			},
		},
		{
			description: "exclusions are case-insensitive AND terms",
			input: url.Values{
				"change_type.filter": []string{"!diff,!Pull*"},
			},
			output: url.Values{
				"and": []string{"(change_type.not.ilike.diff,change_type.not.ilike.Pull*)"},
			},
		},
		{
			description: "positive and negative patterns preserve MatchItem precedence",
			input: url.Values{
				"role.filter": []string{"Owner*,*Reader,!Legacy*"},
			},
			output: url.Values{
				"and": []string{"(or=(role.ilike.Owner*,role.ilike.*Reader),role.not.ilike.Legacy*)"},
			},
		},
		{
			description: "multiple fields and repeated values are combined with AND",
			input: url.Values{
				"role.filter":      []string{"Owner", "Reader"},
				"user_type.filter": []string{"!Service*"},
			},
			output: url.Values{
				"and": []string{"(or=(role.ilike.Owner,role.ilike.Reader),user_type.not.ilike.Service*)"},
			},
		},
		{
			description: "datemath query",
			input: url.Values{
				"created_at.filter": []string{"now-20h"},
			},
			output: url.Values{
				"created_at": []string{fmt.Sprintf(`lt.%s`, now.UTC().Add(-time.Hour*20).Format(time.RFC3339))},
			},
		},
		{
			description: "datemath query with operator",
			input: url.Values{
				"created_at.filter": []string{">now-20h"},
			},
			output: url.Values{
				"created_at": []string{fmt.Sprintf(`gt.%s`, now.UTC().Add(-time.Hour*20).Format(time.RFC3339))},
			},
		},
	}

	for _, d := range testData {
		ginkgo.It(d.description, func() {
			transformQuery, err := transformQuery(now.UTC(), d.input)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(transformQuery).Should(Equal(d.output))
		})
	}

	ginkgo.It("surfaces malformed encoded filters", func() {
		_, err := transformQuery(now.UTC(), url.Values{"name.filter": []string{"%zz"}})

		Expect(err).To(MatchError(ContainSubstring("invalid filter for field name")))
	})
})
