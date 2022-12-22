package snapshot

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
)

func Topology(c echo.Context) error {
	id := c.Param("id")
	related, _ := strconv.ParseBool(c.QueryParam("related"))
	directory := fmt.Sprintf("snapshot-%s", time.Now().Format(time.RFC3339))
	if err := os.MkdirAll(directory, os.ModePerm); err != nil {
		errMsg := []byte(fmt.Sprintf(`{"error": "%v", "message": "error creating directory"}`, err))
		return c.JSONBlob(http.StatusInternalServerError, errMsg)
	}
	if err := topologySnapshot(id, related, directory); err != nil {
		errMsg := []byte(fmt.Sprintf(`{"error": "%v", "message": "error snapshotting topology"}`, err))
		return c.JSONBlob(http.StatusInternalServerError, errMsg)
	}

	return c.File(directory + ".zip")

}

func Incident(c echo.Context) error {
	id := c.Param("id")
	directory := fmt.Sprintf("snapshot-%s", time.Now().Format(time.RFC3339))
	if err := os.MkdirAll(directory, os.ModePerm); err != nil {
		errMsg := []byte(fmt.Sprintf(`{"error": "%v", "message": "error creating directory"}`, err))
		return c.JSONBlob(http.StatusInternalServerError, errMsg)
	}
	if err := incidentSnapshot(id, directory); err != nil {
		errMsg := []byte(fmt.Sprintf(`{"error": "%v", "message": "error snapshotting incident"}`, err))
		return c.JSONBlob(http.StatusInternalServerError, errMsg)
	}

	return c.File(directory + ".zip")
}

func Config(c echo.Context) error {
	id := c.Param("id")
	related, _ := strconv.ParseBool(c.QueryParam("related"))
	directory := fmt.Sprintf("snapshot-%s", time.Now().Format(time.RFC3339))
	if err := os.MkdirAll(directory, os.ModePerm); err != nil {
		errMsg := []byte(fmt.Sprintf(`{"error": "%v", "message": "error creating directory"}`, err))
		return c.JSONBlob(http.StatusInternalServerError, errMsg)
	}
	if err := configSnapshot(id, related, directory); err != nil {
		errMsg := []byte(fmt.Sprintf(`{"error": "%v", "message": "error snapshotting config"}`, err))
		return c.JSONBlob(http.StatusInternalServerError, errMsg)
	}

	return c.File(directory + ".zip")
}
