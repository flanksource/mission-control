package notification_test

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"testing"

	embeddedPG "github.com/fergusstrange/embedded-postgres"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/testutils"
	"github.com/flanksource/incident-commander/db"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestNotifications(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Notification Suite")
}

var (
	postgresServer  *embeddedPG.EmbeddedPostgres
	webhookServer   *http.Server
	webhookEndpoint string            // the autogenerated endpoint for our webhook
	webhookPostdata map[string]string // JSON message sent by shoutrrr to our webhook
)

func setupWebhookServer() {
	listener, err := net.Listen("tcp", ":0") // will assign a random port
	if err != nil {
		ginkgo.Fail(err.Error())
	}

	if addr, ok := listener.Addr().(*net.TCPAddr); ok {
		webhookEndpoint = fmt.Sprintf("http://localhost:%d/webhook", addr.Port)
	} else {
		ginkgo.Fail("unexpected error: failed to parse port.")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		if err := json.NewDecoder(r.Body).Decode(&webhookPostdata); err != nil {
			logger.Errorf("failed to unmarshall: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
	})

	webhookServer = &http.Server{
		Handler: mux,
	}

	go func() {
		defer ginkgo.GinkgoRecover() // Required by ginkgo, if an assertion is made in a goroutine.

		log.Printf("Starting webhookServer on %s", listener.Addr().String())
		if err := webhookServer.Serve(listener); err != nil {
			if err == http.ErrServerClosed {
				logger.Infof("Server closed")
			} else {
				ginkgo.Fail(fmt.Sprintf("Failed to start test server: %v", err))
			}
		}
	}()
}

var _ = ginkgo.BeforeSuite(func() {
	var err error
	port := 9880
	config, connection := testutils.GetEmbeddedPGConfig("test", port)
	postgresServer = embeddedPG.NewDatabase(config)
	if err = postgresServer.Start(); err != nil {
		ginkgo.Fail(err.Error())
	}
	logger.Infof("Started postgres on port: %d", port)

	if db.Gorm, db.Pool, err = duty.SetupDB(connection, nil); err != nil {
		ginkgo.Fail(err.Error())
	}

	setupWebhookServer()
})

var _ = ginkgo.AfterSuite(func() {
	logger.Infof("Stopping postgres")
	if err := postgresServer.Stop(); err != nil {
		ginkgo.Fail(err.Error())
	}

	if err := webhookServer.Close(); err != nil {
		logger.Errorf("Fail to close webhook server: %v", err)
	}
})