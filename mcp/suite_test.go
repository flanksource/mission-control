package mcp

import (
	"net/http/httptest"
	"testing"

	echov4 "github.com/labstack/echo/v4"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/tests/setup"
	echoSrv "github.com/flanksource/incident-commander/echo"
)

func TestMCP(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "MCP")
}

var DefaultContext context.Context

var (
	mcpClient  *mcp.ClientSession
	testServer *httptest.Server
)

var _ = ginkgo.BeforeSuite(func() {
	DefaultContext = setup.BeforeSuiteFn()
	DefaultContext.Logger.SetLogLevel(DefaultContext.Properties().String("log.level", "info"))
	DefaultContext.Infof("%s", DefaultContext.String())

	// Create a test server with the MCP handler
	e := echoSrv.New(DefaultContext)
	mcpServer := Server(DefaultContext)
	e.POST("/mcp", echov4.WrapHandler(mcpServer.HTTPHandler))
	testServer = httptest.NewServer(e)

	// Initialize MCP client
	var err error
	mcpClient, err = initializeTestClient(testServer.URL + "/mcp")
	Expect(err).NotTo(HaveOccurred())
})

var _ = ginkgo.AfterSuite(func() {
	testServer.Close()
	setup.AfterSuiteFn()
})

// Helper function to initialize test client with the new SDK
func initializeTestClient(url string) (*mcp.ClientSession, error) {
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "1.0.0",
	}, nil)

	transport := &mcp.StreamableClientTransport{
		URL: url,
	}

	session, err := client.Connect(DefaultContext, transport, nil)
	if err != nil {
		return nil, err
	}

	return session, nil
}
