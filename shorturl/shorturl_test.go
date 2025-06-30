package shorturl

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/job"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/setup"
	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	echoSrv "github.com/flanksource/incident-commander/echo"
)

func TestURLShortener(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "URL Shortener")
}

var (
	DefaultContext context.Context
	e              *echo.Echo
	server         *httptest.Server
)

var _ = ginkgo.BeforeSuite(func() {
	DefaultContext = setup.BeforeSuiteFn()

	e = echoSrv.New(DefaultContext)
	server = httptest.NewServer(e)
})

var _ = ginkgo.AfterSuite(func() {
	setup.AfterSuiteFn()
	server.Close()
})

var _ = ginkgo.Describe("URL Shortener", func() {
	var testURL = "https://example.com/test"

	ginkgo.Describe("Create", func() {
		ginkgo.It("should create a short URL with default expiration", func() {
			alias, err := Create(DefaultContext, testURL)
			Expect(err).ToNot(HaveOccurred())
			Expect(alias).ToNot(BeNil())
		})

		ginkgo.It("should create a short URL with custom expiration", func() {
			expiresAt := time.Now().Add(1 * time.Hour)
			alias, err := CreateWithExpiry(DefaultContext, testURL, &expiresAt)
			Expect(err).ToNot(HaveOccurred())
			Expect(alias).ToNot(BeNil())
		})

		ginkgo.It("should fail with invalid URL", func() {
			alias, err := Create(DefaultContext, "://invalid-url")
			Expect(err).To(HaveOccurred())
			Expect(alias).To(BeNil())
		})
	})

	ginkgo.Describe("Fetch", func() {
		ginkgo.It("should retrieve URL from cache", func() {
			expiresAt := time.Now().Add(1 * time.Hour)
			aliasPtr, err := CreateWithExpiry(DefaultContext, testURL, &expiresAt)
			Expect(err).ToNot(HaveOccurred())
			alias := *aliasPtr

			cachedItem, found := urlCache.Get(alias)
			Expect(found).To(BeTrue())
			cachedURL, ok := cachedItem.(string)
			Expect(ok).To(BeTrue())
			Expect(cachedURL).To(Equal(testURL))
		})

		ginkgo.It("should handle non-existent alias", func() {
			shortURL, err := Get(DefaultContext, "nonexistent")
			Expect(err).To(HaveOccurred())
			Expect(shortURL).To(BeEmpty())
		})

		ginkgo.It("should handle expired URLs", func() {
			expiresAt := time.Now().Add(50 * time.Millisecond)
			aliasPtr, err := CreateWithExpiry(DefaultContext, testURL, &expiresAt)
			Expect(err).ToNot(HaveOccurred())

			time.Sleep(time.Second)

			shortURL, err := Get(DefaultContext, *aliasPtr)
			Expect(err).To(HaveOccurred())
			Expect(shortURL).To(BeEmpty())
		})
	})

	ginkgo.Describe("HTTP Redirection", func() {
		client := &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}

		ginkgo.It("should redirect to the original URL", func() {
			alias, err := Create(DefaultContext, testURL)
			Expect(err).ToNot(HaveOccurred())

			fullURL, err := url.JoinPath(server.URL, redirectPath, *alias)
			Expect(err).ToNot(HaveOccurred())

			resp, err := client.Get(fullURL)
			Expect(err).ToNot(HaveOccurred())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusFound))
			Expect(resp.Header.Get("Location")).To(Equal(testURL))
		})

		ginkgo.It("should return 404 for non-existent alias", func() {
			fullURL, err := url.JoinPath(server.URL, redirectPath, "nonexistent")
			Expect(err).ToNot(HaveOccurred())

			resp, err := client.Get(fullURL)
			Expect(err).ToNot(HaveOccurred())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	ginkgo.Describe("CleanupExpired", func() {
		ginkgo.It("should clean up expired URLs", func() {
			expiredTime := time.Now().Add(-1 * time.Hour)
			validTime := time.Now().Add(1 * time.Hour)

			expiredAlias, err := CreateWithExpiry(DefaultContext, testURL, &expiredTime)
			Expect(err).ToNot(HaveOccurred())

			validAlias, err := CreateWithExpiry(DefaultContext, testURL, &validTime)
			Expect(err).ToNot(HaveOccurred())

			Expect(CleanupExpired(job.JobRuntime{Context: DefaultContext, History: &models.JobHistory{}})).To(Succeed())

			var expiredURL models.ShortURL
			err = DefaultContext.DB().Where("alias = ?", *expiredAlias).First(&expiredURL).Error
			Expect(err).To(HaveOccurred())

			var validURL models.ShortURL
			err = DefaultContext.DB().Where("alias = ?", *validAlias).First(&validURL).Error
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
