package main

import (
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
)

func TestFaro(t *testing.T) {
	// Gomega renders a failed error assertion by reflecting over the error's internals, which for
	// oops context and goccy YAML tokens means stack frames, timezone transition tables and a
	// doubly linked token stream — hundreds of lines that say nothing the message does not.
	format.RegisterCustomFormatter(func(value any) (string, bool) {
		err, ok := value.(error)
		if !ok {
			return "", false
		}
		return err.Error(), true
	})

	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Faro")
}
