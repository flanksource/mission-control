package events

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	embeddedPG "github.com/fergusstrange/embedded-postgres"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/fixtures/dummy"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/testutils"
	"github.com/flanksource/incident-commander/api"
	"github.com/google/uuid"

	"github.com/flanksource/incident-commander/upstream"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

func TestEvents(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Events test suite")
}

type agentWrapper struct {
	id      uuid.UUID // agent's id in the upstream db
	name    string
	db      *gorm.DB
	pool    *pgxpool.Pool
	dataset dummy.DummyData
}

func (t *agentWrapper) setup(connection string) {
	var err error

	if t.db, t.pool, err = duty.SetupDB(connection, nil); err != nil {
		ginkgo.Fail(err.Error())
	}

	if err := t.dataset.Populate(t.db); err != nil {
		ginkgo.Fail(err.Error())
	}
}

func (t *agentWrapper) stop() {
	t.pool.Close()
}

var (
	// postgres server shared by both agent and upstream
	postgresServer *embeddedPG.EmbeddedPostgres
	pgServerPort   = 9879

	upstreamEchoServerport = 11005
	upstreamEchoServer     *echo.Echo

	agentBob   = agentWrapper{name: "bob", id: uuid.New(), dataset: dummy.GenerateDynamicDummyData()}
	agentJames = agentWrapper{name: "james", id: uuid.New(), dataset: dummy.GenerateDynamicDummyData()}
	agentRoss  = agentWrapper{name: "ross", id: uuid.New(), dataset: dummy.GenerateDynamicDummyData()}

	playbookDB     *gorm.DB
	playbookDBPool *pgxpool.Pool

	upstreamDB       *gorm.DB
	upstreamDBPGPool *pgxpool.Pool
	upstreamDBName   = "upstream"
)

var _ = ginkgo.BeforeSuite(func() {
	config, connection := testutils.GetEmbeddedPGConfig(agentBob.name, pgServerPort)
	postgresServer = embeddedPG.NewDatabase(config)
	if err := postgresServer.Start(); err != nil {
		ginkgo.Fail(err.Error())
	}
	logger.Infof("Started postgres on port: %d", pgServerPort)

	agentBob.setup(connection)

	// Setup another agent
	_, err := agentBob.pool.Exec(context.TODO(), fmt.Sprintf("CREATE DATABASE %s", agentJames.name))
	Expect(err).NotTo(HaveOccurred())
	agentJames.setup(strings.ReplaceAll(connection, agentBob.name, agentJames.name))

	_, err = agentBob.pool.Exec(context.TODO(), fmt.Sprintf("CREATE DATABASE %s", agentRoss.name))
	Expect(err).NotTo(HaveOccurred())
	agentRoss.setup(strings.ReplaceAll(connection, agentBob.name, agentRoss.name))

	// Setup upstream db
	_, err = agentBob.pool.Exec(context.TODO(), fmt.Sprintf("CREATE DATABASE %s", upstreamDBName))
	Expect(err).NotTo(HaveOccurred())
	upstreamDBConnection := strings.ReplaceAll(connection, agentBob.name, upstreamDBName)
	if upstreamDB, upstreamDBPGPool, err = duty.SetupDB(upstreamDBConnection, nil); err != nil {
		ginkgo.Fail(err.Error())
	}

	Expect(upstreamDB.Create(&models.Agent{ID: agentBob.id, Name: agentBob.name}).Error).To(BeNil())
	Expect(upstreamDB.Create(&models.Agent{ID: agentJames.id, Name: agentJames.name}).Error).To(BeNil())
	Expect(upstreamDB.Create(&models.Agent{ID: agentRoss.id, Name: agentRoss.name}).Error).To(BeNil())

	// Setup database for playbook
	_, err = agentBob.pool.Exec(context.TODO(), "CREATE DATABASE playbook")
	Expect(err).NotTo(HaveOccurred())
	playbookDBConnection := strings.ReplaceAll(connection, agentBob.name, "playbook")
	if playbookDB, playbookDBPool, err = duty.SetupDB(playbookDBConnection, nil); err != nil {
		ginkgo.Fail(err.Error())
	}

	upstreamEchoServer = echo.New()
	upstreamEchoServer.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			cc := api.NewContext(upstreamDB, upstreamDBPGPool).WithEchoContext(c)
			return next(cc)
		}
	})
	upstreamEchoServer.POST("/upstream/push", upstream.PushUpstream)
	listenAddr := fmt.Sprintf(":%d", upstreamEchoServerport)

	go func() {
		defer ginkgo.GinkgoRecover() // Required by ginkgo, if an assertion is made in a goroutine.
		if err := upstreamEchoServer.Start(listenAddr); err != nil {
			if err == http.ErrServerClosed {
				logger.Infof("Server closed")
			} else {
				ginkgo.Fail(fmt.Sprintf("Failed to start test server: %v", err))
			}
		}
	}()
})

var _ = ginkgo.AfterSuite(func() {
	agentBob.stop()
	agentJames.stop()

	logger.Infof("Stopping upstream echo server")
	if err := upstreamEchoServer.Shutdown(context.Background()); err != nil {
		ginkgo.Fail(err.Error())
	}

	logger.Infof("Stopping postgres")
	if err := postgresServer.Stop(); err != nil {
		ginkgo.Fail(err.Error())
	}
})
