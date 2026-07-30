package v1

import (
	"encoding/json"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("TemplatedBool", func() {
	ginkgo.It("documents literal booleans and template strings in JSON schema", func() {
		schema := (TemplatedBool(nil)).JSONSchema()
		Expect(schema.OneOf).To(HaveLen(2))
		Expect(schema.OneOf[0].Type).To(Equal("boolean"))
		Expect(schema.OneOf[1].Type).To(Equal("string"))
	})

	resolveTests := []struct {
		name         string
		input        string
		defaultValue bool
		expected     bool
	}{
		{name: "literal true", input: `true`, expected: true},
		{name: "literal false", input: `false`, expected: false},
		{name: "rendered true", input: `"true"`, expected: true},
		{name: "rendered false", input: `"false"`, expected: false},
		{name: "rendered value ignores whitespace and case", input: `" TRUE "`, expected: true},
		{name: "null uses the default", input: `null`, defaultValue: true, expected: true},
	}

	for _, tt := range resolveTests {
		ginkgo.It("resolves "+tt.name, func() {
			var value TemplatedBool
			Expect(json.Unmarshal([]byte(tt.input), &value)).To(Succeed())

			resolved, err := value.Resolve(tt.defaultValue)
			Expect(err).NotTo(HaveOccurred())
			Expect(resolved).To(Equal(tt.expected))
		})
	}

	invalidJSON := []string{`1`, `{}`, `[]`}
	for _, input := range invalidJSON {
		ginkgo.It("rejects non-boolean, non-string JSON "+input, func() {
			var value TemplatedBool
			Expect(json.Unmarshal([]byte(input), &value)).To(MatchError("expected true, false, or a template string"))
		})
	}

	ginkgo.It("preserves a literal boolean when marshaled", func() {
		var value TemplatedBool
		Expect(json.Unmarshal([]byte(`true`), &value)).To(Succeed())

		data, err := json.Marshal(value)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).To(Equal(`true`))
	})

	ginkgo.It("preserves a template string when marshaled", func() {
		var value TemplatedBool
		Expect(json.Unmarshal([]byte(`"{{ .params.enabled }}"`), &value)).To(Succeed())

		data, err := json.Marshal(value)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).To(Equal(`"{{ .params.enabled }}"`))
	})

	ginkgo.It("uses the default for an unset value", func() {
		resolved, err := (TemplatedBool(nil)).Resolve(true)
		Expect(err).NotTo(HaveOccurred())
		Expect(resolved).To(BeTrue())
	})

	ginkgo.It("rejects a rendered value other than true or false", func() {
		var value TemplatedBool
		Expect(json.Unmarshal([]byte(`"sometimes"`), &value)).To(Succeed())

		_, err := value.Resolve(false)
		Expect(err).To(MatchError(`template rendered "sometimes"; expected true or false`))
	})
})
