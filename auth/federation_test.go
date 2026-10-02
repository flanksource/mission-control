package auth

import (
	"net/http"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Signing key redirects", func() {
	requests := func(urls ...string) []*http.Request {
		var reqs []*http.Request
		for _, u := range urls {
			req, err := http.NewRequest(http.MethodGet, u, nil)
			Expect(err).ToNot(HaveOccurred())
			reqs = append(reqs, req)
		}
		return reqs
	}

	check := func(to string, via ...string) error {
		return keyFetchClient.CheckRedirect(requests(to)[0], requests(via...))
	}

	ginkgo.It("follows five redirects, and refuses a sixth", func() {
		hops := []string{"https://a.example.com/0"}
		for i := 1; i <= maxKeyRedirects; i++ {
			Expect(check("https://a.example.com/next", hops...)).To(Succeed(), "redirect %d", i)
			hops = append(hops, "https://a.example.com/next")
		}
		Expect(check("https://a.example.com/next", hops...)).ToNot(Succeed())
	})

	ginkgo.It("refuses a redirect from https to http, even between loopback hosts", func() {
		Expect(check("http://auth.example.com/keys", "https://auth.example.com/keys")).ToNot(Succeed())
		Expect(check("http://localhost/keys", "https://localhost/keys")).ToNot(Succeed())
		Expect(check("http://localhost/keys", "http://localhost/start", "https://localhost/keys")).ToNot(Succeed())
	})

	ginkgo.It("follows plain http only to a loopback host", func() {
		Expect(check("http://127.0.0.1/keys", "http://localhost/keys")).To(Succeed())
		Expect(check("http://auth.example.com/keys", "http://localhost/keys")).ToNot(Succeed())
	})
})
