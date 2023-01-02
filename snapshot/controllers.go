package snapshot

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/flanksource/incident-commander/api"
	"github.com/labstack/echo/v4"
)

func Topology(c echo.Context) error {
	id := c.Param("id")
	related, _ := strconv.ParseBool(c.QueryParam("related"))
	logStart := c.QueryParam("logStart")
	logEnd := c.QueryParam("logEnd")
	if logStart == "" {
		logStart = "15m"
	}
	directory := fmt.Sprintf("snapshot-%s", time.Now().Format(time.RFC3339))
	if err := os.MkdirAll(directory, os.ModePerm); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPErrorMessage{
			Error:   err.Error(),
			Message: "Error creating directory",
		})
	}

	ctx := SnapshotContext{
		Directory: directory,
		LogStart:  logStart,
		LogEnd:    logEnd,
	}
	if err := topologySnapshot(ctx, id, related); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPErrorMessage{
			Error:   err.Error(),
			Message: "Error snapshotting topology",
		})
	}

	defer os.RemoveAll(directory)
	defer os.Remove(directory + ".zip")
	return c.File(directory + ".zip")
}

func Incident(c echo.Context) error {
	id := c.Param("id")
	directory := fmt.Sprintf("snapshot-%s", time.Now().Format(time.RFC3339))
	if err := os.MkdirAll(directory, os.ModePerm); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPErrorMessage{
			Error:   err.Error(),
			Message: "Error creating directory",
		})
	}

	ctx := SnapshotContext{Directory: directory}
	if err := incidentSnapshot(ctx, id); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPErrorMessage{
			Error:   err.Error(),
			Message: "Error snapshotting incident",
		})
	}

	defer os.RemoveAll(directory)
	defer os.Remove(directory + ".zip")
	return c.File(directory + ".zip")
}

func Config(c echo.Context) error {
	id := c.Param("id")
	related, _ := strconv.ParseBool(c.QueryParam("related"))
	directory := fmt.Sprintf("snapshot-%s", time.Now().Format(time.RFC3339))
	if err := os.MkdirAll(directory, os.ModePerm); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPErrorMessage{
			Error:   err.Error(),
			Message: "Error creating directory",
		})
	}

	ctx := SnapshotContext{Directory: directory}
	if err := configSnapshot(ctx, id, related); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPErrorMessage{
			Error:   err.Error(),
			Message: "Error snapshotting config",
		})
	}

	defer os.RemoveAll(directory)
	defer os.Remove(directory + ".zip")
	return c.File(directory + ".zip")
}
