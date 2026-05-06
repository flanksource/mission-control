package main

import (
	"context"
	"net"

	"github.com/google/gops/signal"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("GopsClient", func() {
	ginkgo.It("sends signal bytes and reads the response", func() {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).ToNot(HaveOccurred())
		defer ln.Close()

		done := make(chan byte, 1)
		go func() {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
			buf := make([]byte, 1)
			_, _ = conn.Read(buf)
			done <- buf[0]
			_, _ = conn.Write([]byte("go1.test\n"))
		}()

		got, err := (GopsClient{Addr: ln.Addr().String()}).Version(context.Background())
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(Equal("go1.test\n"))
		Expect(<-done).To(Equal(signal.Version))
	})
})
