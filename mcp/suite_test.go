package mcp

import (
	"net/http/httptest"
	"testing"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/tests/setup"
	echov4 "github.com/labstack/echo/v4"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

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
	mcpServer  *MCPServer
)

var _ = ginkgo.BeforeSuite(func() {
	DefaultContext = setup.BeforeSuiteFn()
	DefaultContext.Logger.SetLogLevel(DefaultContext.Properties().String("log.level", "info"))
	DefaultContext.Infof("%s", DefaultContext.String())

	// Create a test server with the MCP handler
	e := echoSrv.New(DefaultContext)

	// Use stateless mode to prevent sporadic panics during test cleanup.
	//
	// The default SSE mode spawns a goroutine per request that waits for notifications.
	// This goroutine has a deferred flusher.Flush() that runs when the goroutine exits.
	// When tests complete and httptest.Server.Close() is called, the response writer
	// is closed before the goroutine exits, causing the deferred flush to panic with
	// "invalid memory address or nil pointer dereference".
	//
	// Stateless mode handles each request synchronously without persistent goroutines,
	// eliminating the race between server shutdown and goroutine cleanup.
	mcpServer = Server(DefaultContext, server.WithStateLess(true))

	e.POST("/mcp", echov4.WrapHandler(mcpServer.HTTPHandler))
	testServer = httptest.NewServer(e)

	// Initialize MCP client
	var err error
	mcpClient, err = client.NewStreamableHttpClient(testServer.URL + "/mcp")
	Expect(err).NotTo(HaveOccurred())

	_, err = mcpClient.Initialize(DefaultContext, mcp.InitializeRequest{})
	Expect(err).NotTo(HaveOccurred())
})

var _ = ginkgo.AfterSuite(func() {
	// Close the client first to ensure all goroutines finish before closing the server
	if mcpClient != nil {
		mcpClient.Close()
	}
	if testServer != nil {
		testServer.Close()
	}
	setup.AfterSuiteFn()
})
