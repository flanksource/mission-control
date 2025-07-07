package prometheus

import (
	"crypto/tls"
	"fmt"
	"net/http"

	"github.com/flanksource/duty/connection"
	dutycontext "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
)

func NewPrometheusClient(ctx dutycontext.Context, connectionString string) (v1.API, error) {
	conn, err := connection.Get(ctx, connectionString)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection %s: %w", connectionString, err)
	}

	if conn.Type != models.ConnectionTypePrometheus {
		return nil, fmt.Errorf("connection of type %s cannot be used with prometheus", conn.Type)
	}

	transport := &http.Transport{}
	if conn.Properties["insecure_tls"] == "true" {
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	}

	var roundTripper http.RoundTripper = transport
	if conn.Username != "" || conn.Password != "" {
		roundTripper = &basicAuthRoundTripper{
			username: conn.Username,
			password: conn.Password,
			base:     roundTripper,
		}
	}

	cfg := api.Config{
		Address:      conn.URL,
		RoundTripper: roundTripper,
	}

	client, err := api.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Prometheus client: %w", err)
	}

	return v1.NewAPI(client), nil
}

type basicAuthRoundTripper struct {
	username, password string
	base               http.RoundTripper
}

func (rt *basicAuthRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.SetBasicAuth(rt.username, rt.password)
	return rt.base.RoundTrip(req)
}
