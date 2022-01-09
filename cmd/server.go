package cmd

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/incident-commander/db"
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
		e.GET("/", func(c echo.Context) error {
			return c.String(http.StatusOK, "Hello, World!")
		})
		// PostgREST needs to know how it is exposed to create the correct links
		db.HttpEndpoint = publicEndpoint + "/db"
		go db.StartPostgrest()

		url, err := url.Parse("http://localhost:3000")
		if err != nil {
			e.Logger.Fatal(err)
		}

		e.Use(middleware.Logger())

		e.Group("/db").Use(middleware.ProxyWithConfig(middleware.ProxyConfig{
			Rewrite: map[string]string{
				"^/db/*": "/$1",
			},
			Balancer: middleware.NewRoundRobinBalancer([]*middleware.ProxyTarget{
				{
					URL: url,
				},
			}),
		}))
		if err := e.Start(fmt.Sprintf(":%d", httpPort)); err != nil {
			e.Logger.Fatal(err)
		}
	},
}

func init() {
	ServerFlags(Serve.Flags())
}
