package snapshot

import (
	"fmt"
	"net/http"
	"os"
	"strconv"

	commonUtils "github.com/flanksource/commons/utils"
	"github.com/labstack/echo/v4"
)

func Topology(c echo.Context) error {
	id := c.Param("id")
	related, _ := strconv.ParseBool(c.QueryParam("related"))
	directory := commonUtils.RandomString(12)
	if err := os.MkdirAll(directory, os.ModePerm); err != nil {
		errMsg := []byte(fmt.Sprintf(`{"error": "%v", "message": "error creating directory"}`, err))
		return c.JSONBlob(http.StatusInternalServerError, errMsg)
	}
	if err := topologySnapshot(id, related, directory); err != nil {
		errMsg := []byte(fmt.Sprintf(`{"error": "%v", "message": "error snapshotting topology"}`, err))
		return c.JSONBlob(http.StatusInternalServerError, errMsg)
	}

	return c.String(http.StatusOK, "Stream archive")
}
