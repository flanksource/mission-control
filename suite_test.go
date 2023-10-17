package main

import (
	"testing"

	embeddedPG "github.com/fergusstrange/embedded-postgres"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/testutils"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel"
)

func TestMissionControl(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Mission Control Suite")
}

var (
	postgresServer *embeddedPG.EmbeddedPostgres
)

var _ = ginkgo.BeforeSuite(func() {
	var err error
	port := 9881
	config, connection := testutils.GetEmbeddedPGConfig("test", port)
	postgresServer = embeddedPG.NewDatabase(config)
	if err := postgresServer.Start(); err != nil {
		ginkgo.Fail(err.Error())
	}
	logger.Infof("Started postgres on port: %d", port)

	if db.Gorm, db.Pool, err = duty.SetupDB(connection, nil); err != nil {
		ginkgo.Fail(err.Error())
	}

	api.DefaultContext = api.NewContext(db.Gorm, db.Pool)
	api.ContextWrapFunc = context.WrapContext(db.Gorm, db.Pool, api.Kubernetes, otel.GetTracerProvider().Tracer("test"))
})

var _ = ginkgo.AfterSuite(func() {
	logger.Infof("Stopping postgres")
	if err := postgresServer.Stop(); err != nil {
		ginkgo.Fail(err.Error())
	}
})
