package snapshot

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/labstack/echo/v4"
)

const (
	LogFormatLog  = "log"
	LogFormatJSON = "json"
)

type SnapshotContext struct {
	Context   context.Context
	Directory string
	LogStart  string
	LogEnd    string
	LogFormat string
}

func NewSnapshotContext(c echo.Context) SnapshotContext {
	ctx := c.Request().Context().(context.Context)
	directory := fmt.Sprintf("snapshot-%s", time.Now().Format(time.RFC3339))
	logStart := c.QueryParam("start")
	logEnd := c.QueryParam("end")
	logFormat := c.QueryParam("logFormat")
	if !collections.Contains([]string{LogFormatLog, LogFormatJSON}, logFormat) {
		logFormat = LogFormatLog
	}

	if logStart == "" {
		logStart = "15m"
	}
	return SnapshotContext{
		Context:   ctx,
		Directory: directory,
		LogStart:  logStart,
		LogEnd:    logEnd,
		LogFormat: logFormat,
	}
}

func Topology(c echo.Context) error {
	id := c.Param("id")
	related, _ := strconv.ParseBool(c.QueryParam("related"))
	ctx := NewSnapshotContext(c)
	if err := os.MkdirAll(ctx.Directory, os.ModePerm); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{
			Error:   err.Error(),
			Message: "Error creating directory",
		})
	}

	if err := topologySnapshot(ctx, id, related); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{
			Error:   err.Error(),
			Message: "Error snapshotting topology",
		})
	}

	defer os.RemoveAll(ctx.Directory)
	defer os.Remove(ctx.Directory + ".zip")
	return c.File(ctx.Directory + ".zip")
}

func Incident(c echo.Context) error {
	id := c.Param("id")

	ctx := NewSnapshotContext(c)
	if err := os.MkdirAll(ctx.Directory, os.ModePerm); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{
			Error:   err.Error(),
			Message: "Error creating directory",
		})
	}

	if err := incidentSnapshot(ctx, id); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{
			Error:   err.Error(),
			Message: "Error snapshotting incident",
		})
	}

	defer os.RemoveAll(ctx.Directory)
	defer os.Remove(ctx.Directory + ".zip")
	return c.File(ctx.Directory + ".zip")
}

func Config(c echo.Context) error {
	id := c.Param("id")
	related, _ := strconv.ParseBool(c.QueryParam("related"))
	ctx := NewSnapshotContext(c)
	if err := os.MkdirAll(ctx.Directory, os.ModePerm); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{
			Error:   err.Error(),
			Message: "Error creating directory",
		})
	}

	if err := configSnapshot(ctx, id, related); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{
			Error:   err.Error(),
			Message: "Error snapshotting config",
		})
	}

	defer os.RemoveAll(ctx.Directory)
	defer os.Remove(ctx.Directory + ".zip")
	return c.File(ctx.Directory + ".zip")
}
