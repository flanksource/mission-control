package notification_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"testing"

	embeddedPG "github.com/fergusstrange/embedded-postgres"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/testutils"
	"github.com/jackc/pgx/v5/pgxpool"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

func TestNotifications(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Notification Suite")
}

var (
	postgresServer  *embeddedPG.EmbeddedPostgres
	webhookEndpoint string
	webhookServer   *http.Server
	webhookCalled   bool
)

func setupDB(connectionString string) (*gorm.DB, *pgxpool.Pool) {
	pgxpool, err := duty.NewPgxPool(connectionString)
	if err != nil {
		ginkgo.Fail(err.Error())
	}
	conn, err := pgxpool.Acquire(context.Background())
	if err != nil {
		ginkgo.Fail(err.Error())
	}
	defer conn.Release()

	gormDB, err := duty.NewGorm(connectionString, duty.DefaultGormConfig())
	if err != nil {
		ginkgo.Fail(err.Error())
	}

	if err = duty.Migrate(connectionString, nil); err != nil {
		ginkgo.Fail(err.Error())
	}

	return gormDB, pgxpool
}

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

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			logger.Errorf("failed to unmarshall: %v", err)
			return
		}

		webhookCalled = true
		logger.Infof("%v", body)
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
	postgresServer = embeddedPG.NewDatabase(embeddedPG.DefaultConfig().Database("test").Port(testutils.TestPostgresPort).Logger(io.Discard))
	if err := postgresServer.Start(); err != nil {
		ginkgo.Fail(err.Error())
	}
	logger.Infof("Started postgres on port: %d", testutils.TestPostgresPort)

	db.Gorm, db.Pool = setupDB(testutils.PGUrl)

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
