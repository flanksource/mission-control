package main

import (
	"errors"

	"github.com/goccy/go-yaml"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/oops"
)

// brokenFlowSequence omits the separator between two flow entries. goccy rejects it at the
// second "[", which sits at column 16 of the only line.
const brokenFlowSequence = "entries: [ [1] [2] ]\n"

var _ = ginkgo.Describe("Projection YAML errors", func() {
	ginkgo.It("does not expose the goccy token graph to reflection-based renderers", func() {
		_, _, _, err := projectionTarget([]byte(brokenFlowSequence), "$.entries[*]")

		Expect(err).To(HaveOccurred())
		var yamlErr yaml.Error
		Expect(errors.As(err, &yamlErr)).To(BeFalse(), "goccy *token.Token is a doubly linked list over the whole document and must not stay reachable")
	})

	ginkgo.It("keeps the goccy token position as structured oops context", func() {
		_, _, _, err := projectionTarget([]byte(brokenFlowSequence), "$.entries[*]")

		Expect(err).To(HaveOccurred())
		oopsErr, ok := oops.AsOops(err)
		Expect(ok).To(BeTrue())
		Expect(oopsErr.Context()).To(SatisfyAll(
			HaveKeyWithValue("yaml.line", 1),
			HaveKeyWithValue("yaml.column", 16),
			HaveKeyWithValue("yaml.token", "["),
			HaveKeyWithValue("yaml.type", "*errors.SyntaxError"),
		))
	})

	ginkgo.It("keeps the goccy annotated source excerpt in the message", func() {
		_, _, _, err := projectionTarget([]byte(brokenFlowSequence), "$.entries[*]")

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(SatisfyAll(
			ContainSubstring("',' or ']' must be specified"),
			ContainSubstring("[1:16]"),
			ContainSubstring(">  1 | entries: [ [1] [2] ]"),
		))
	})

	ginkgo.It("returns non-YAML errors unchanged", func() {
		sentinel := errors.New("not a yaml failure")

		Expect(projectionYAMLError(sentinel)).To(BeIdenticalTo(sentinel))
		Expect(projectionYAMLError(nil)).To(BeNil())
	})
})
