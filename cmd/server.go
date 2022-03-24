package cmd

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/ui"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/spf13/cobra"
)

var Serve = &cobra.Command{
	Use: "serve",
	Run: func(cmd *cobra.Command, args []string) {
		if err := db.Init(db.ConnectionString); err != nil {
			logger.Errorf("Failed to initialize the db: %v", err)
		}
		e := echo.New()
		// PostgREST needs to know how it is exposed to create the correct links
		db.HttpEndpoint = publicEndpoint + "/db"
		go db.StartPostgrest()

		e.Use(middleware.Logger())

		forward(e, "/db", "http://localhost:3000")
		forward(e, "/config", configDb)
		forward(e, "/canary", canaryChecker)
		forward(e, "/apm", apmHub)

		contentHandler := echo.WrapHandler(http.FileServer(http.FS(ui.StaticContent)))
		var contentRewrite = middleware.Rewrite(map[string]string{"/*": "/build/$1"})
		e.GET("/*", contentHandler, contentRewrite)
		if err := e.Start(fmt.Sprintf(":%d", httpPort)); err != nil {
			e.Logger.Fatal(err)
		}
	},
}

func forward(e *echo.Echo, prefix string, target string) {
	_url, err := url.Parse(target)
	if err != nil {
		e.Logger.Fatal(err)
	}
	e.Group(prefix).Use(middleware.ProxyWithConfig(middleware.ProxyConfig{
		Rewrite: map[string]string{
			fmt.Sprintf("^%s/*", prefix): "/$1",
		},
		Balancer: middleware.NewRoundRobinBalancer([]*middleware.ProxyTarget{
			{
				URL: _url,
			},
		}),
	}))
}

func init() {
	ServerFlags(Serve.Flags())
}
