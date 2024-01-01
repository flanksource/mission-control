package api

import (
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var fixtures = []struct {
	Items   []string
	Item    string
	Matches bool
}{
	{[]string{"!b"}, "a", true},
	{[]string{"!b"}, "b", false},
	{[]string{"b"}, "b", true},
	{[]string{"b", "c"}, "c", true},
	{[]string{"!b", "*"}, "c", true},
	{[]string{}, "c", true},
	{[]string{"b", "c"}, "", false},
}

var _ = ginkgo.Describe("Items", func() {
	for _, f := range fixtures {
		items := Items(f.Items)
		Expect(items.Contains(f.Item)).To(Equal(f.Matches))
	}
})
