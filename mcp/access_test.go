package mcp

import (
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("parseDuration", func() {
	tests := []struct {
		name     string
		input    string
		expected time.Duration
		hasError bool
	}{
		{name: "days", input: "90d", expected: 90 * 24 * time.Hour},
		{name: "single day", input: "1d", expected: 24 * time.Hour},
		{name: "weeks", input: "2w", expected: 14 * 24 * time.Hour},
		{name: "hours via Go duration", input: "24h", expected: 24 * time.Hour},
		{name: "minutes via Go duration", input: "30m", expected: 30 * time.Minute},
		{name: "empty string defaults to 90d", input: "", expected: 90 * 24 * time.Hour},
		{name: "invalid string", input: "invalid", hasError: true},
		{name: "single char", input: "x", hasError: true},
		{name: "unsupported unit", input: "5z", hasError: true},
	}

	for _, tt := range tests {
		ginkgo.It(tt.name, func() {
			result, err := parseDuration(tt.input)
			if tt.hasError {
				Expect(err).To(HaveOccurred())
			} else {
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(tt.expected))
			}
		})
	}
})
