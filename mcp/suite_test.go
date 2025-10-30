package mcp

import (
	"net/http/httptest"
	"testing"

	echov4 "github.com/labstack/echo/v4"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
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
	mcpClient  *client.Client
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
	mcpClient, err = client.NewStreamableHttpClient(testServer.URL + "/mcp")
	Expect(err).NotTo(HaveOccurred())

	// Start the client before initializing (required in v0.42.0+)
	err = mcpClient.Start(DefaultContext)
	Expect(err).NotTo(HaveOccurred())

	_, err = mcpClient.Initialize(DefaultContext, mcp.InitializeRequest{})
	Expect(err).NotTo(HaveOccurred())
})

var _ = ginkgo.AfterSuite(func() {
	if mcpClient != nil {
		mcpClient.Close()
	}
	testServer.Close()
	setup.AfterSuiteFn()
})
