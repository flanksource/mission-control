package db

import (
	"fmt"
	"net/url"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Transform Query to postgREST", ginkgo.Ordered, func() {
	testData := []struct {
		description string
		input       url.Values
		output      url.Values
	}{
		{
			description: "IN Query",
			input: url.Values{
				"change_type":        []string{"eq=diff"},
				"config_type.filter": []string{"Kubernetes::Pod,Kubernetes::Deployment"},
			},
			output: url.Values{
				"change_type": []string{"eq=diff"},
				"config_type": []string{`in.("Kubernetes::Pod","Kubernetes::Deployment")`},
			},
		},
		{
			description: "NOT IN Query",
			input: url.Values{
				"change_type.filter": []string{"!diff,!Pulled"},
			},
			output: url.Values{
				"change_type": []string{`not.in.("diff","Pulled")`},
			},
		},
		{
			description: "Prefix & Suffix",
			input: url.Values{
				"change_type.filter": []string{"Pull*,ed*"},
			},
			output: url.Values{
				"change_type": []string{`like.Pull*`, `like.ed*`},
			},
		},
		{
			description: "datemath query",
			input: url.Values{
				"created_at.filter": []string{"now-20h"},
			},
			output: url.Values{
				"created_at": []string{fmt.Sprintf(`lt.%s`, time.Now().UTC().Add(-time.Hour*20).Format(time.RFC3339))},
			},
		},
		{
			description: "datemath query with operator",
			input: url.Values{
				"created_at.filter": []string{">now-20h"},
			},
			output: url.Values{
				"created_at": []string{fmt.Sprintf(`gt.%s`, time.Now().UTC().Add(-time.Hour*20).Format(time.RFC3339))},
			},
		},
	}

	for _, d := range testData {
		ginkgo.It(d.description, func() {
			transformQuery, err := transformQuery(d.input)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(transformQuery).Should(Equal(d.output))
		})
	}
})
