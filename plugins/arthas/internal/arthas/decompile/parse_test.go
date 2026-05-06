package decompile

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("JAD parsing", func() {
	ginkgo.It("maps arthas source comments back to line numbers", func() {
		lines := ParseJad(`/* 10 */ public class Demo {
/* 11 */   void run() {}
/* 12 */ }`)
		Expect(lines[10]).To(Equal(" public class Demo {"))
		Expect(lines[11]).To(Equal("   void run() {}"))
	})
})
