package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	pkgView "github.com/flanksource/duty/view"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/yaml"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/views"
)

// prometheusResponse represents the expected Prometheus API response format
type prometheusResponse struct {
	Status string                 `json:"status"`
	Data   prometheusResponseData `json:"data"`
}

type prometheusResponseData struct {
	ResultType string                   `json:"resultType"`
	Result     []prometheusResultVector `json:"result"`
}

type prometheusResultVector struct {
	Metric map[string]string `json:"metric"`
	Value  []any             `json:"value"`
}

// startDummyPrometheusServer starts a dummy HTTP server on port 9090 that mimics Prometheus API
func startDummyPrometheusServer() *http.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/query", func(w http.ResponseWriter, r *http.Request) {
		response := prometheusResponse{
			Status: "success",
			Data: prometheusResponseData{
				ResultType: "vector",
				Result: []prometheusResultVector{
					{
						Metric: map[string]string{
							"namespace": dummy.LogisticsAPIPodConfig.GetNamespace(),
							"pod":       *dummy.LogisticsAPIPodConfig.Name,
						},
						Value: []any{time.Now().Unix(), "128"},
					},
					{
						Metric: map[string]string{
							"namespace": dummy.LogisticsUIPodConfig.GetNamespace(),
							"pod":       *dummy.LogisticsUIPodConfig.Name,
						},
						Value: []any{time.Now().Unix(), "64"},
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(response)
		if err != nil {
			ginkgo.Fail(fmt.Sprintf("Failed to encode Prometheus response: %v", err))
		}
	})

	server := &http.Server{
		Addr:    ":9090",
		Handler: mux,
	}

	go func() {
		defer ginkgo.GinkgoRecover() // Required by ginkgo, if an assertion is made in a goroutine.)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			ginkgo.Fail(fmt.Sprintf("Dummy Prometheus server failed to start: %v", err))
		}
	}()

	return server
}

var _ = ginkgo.Describe("Views E2E", ginkgo.Ordered, func() {
	var (
		prometheusConnection  *v1.Connection
		dummyPrometheusServer *http.Server
	)

	ginkgo.BeforeAll(func() {
		// Start dummy Prometheus server
		dummyPrometheusServer = startDummyPrometheusServer()

		// Save connection
		connectionData, err := os.ReadFile("testdata/prometheus.yaml")
		Expect(err).NotTo(HaveOccurred())
		err = yaml.Unmarshal(connectionData, &prometheusConnection)
		Expect(err).NotTo(HaveOccurred())
		err = db.PersistConnectionFromCRD(DefaultContext, prometheusConnection)
		Expect(err).NotTo(HaveOccurred())
		Expect(rbac.ReloadPolicy()).To(Succeed())
	})

	ginkgo.AfterAll(func() {
		// Cleanup connection
		DefaultContext.DB().Delete(&models.Connection{}, "name = ? AND namespace = ?", "prometheus", "mc")

		// Stop dummy Prometheus server
		if dummyPrometheusServer != nil {
			dummyPrometheusServer.Close()
		}
	})

	ginkgo.It("should execute view and query prometheus metrics", func() {
		viewPath := "testdata/pod-metric.yaml"
		view, err := LoadViewFromFile(viewPath)
		Expect(err).NotTo(HaveOccurred())

		err = db.PersistViewFromCRD(DefaultContext, view)
		Expect(err).NotTo(HaveOccurred())

		result, err := views.ReadOrPopulateViewTable(DefaultContext.WithUser(&dummy.AlanTuring), view.GetNamespace(), view.GetName(), views.WithIncludeRows(true))
		Expect(err).NotTo(HaveOccurred())

		expectedRows := []pkgView.Row{
			{
				*dummy.LogisticsAPIPodConfig.Name,
				dummy.LogisticsAPIPodConfig.GetNamespace(),
				"Running",
				"healthy",
				"128.0/256Mi",
				nil,
			},
			{
				*dummy.LogisticsUIPodConfig.Name,
				dummy.LogisticsUIPodConfig.GetNamespace(),
				"Running",
				"healthy",
				"64.0/128Mi",
				nil,
			},
		}
		Expect(result.Rows).To(Equal(expectedRows))
	})
})

// LoadViewFromFile loads a View from a YAML file
func LoadViewFromFile(filePath string) (*v1.View, error) {
	yamlData, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var view v1.View
	err = yaml.Unmarshal(yamlData, &view)
	if err != nil {
		return nil, err
	}

	return &view, nil
}
