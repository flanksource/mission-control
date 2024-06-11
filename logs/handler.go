package logs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/flanksource/commons/collections"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/duty/types"
	"github.com/labstack/echo/v4"

	"github.com/flanksource/incident-commander/api"
)

func LogsHandler(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)
	var reqData struct {
		ID     string            `json:"id"`
		Name   string            `json:"name"`
		Labels map[string]string `json:"labels"`
	}
	if err := c.Bind(&reqData); err != nil {
		return c.JSON(http.StatusBadRequest, dutyAPI.HTTPError{
			Err:     err.Error(),
			Message: "Invalid request body",
		})
	}

	if reqData.ID == "" {
		return c.JSON(http.StatusBadRequest, dutyAPI.HTTPError{
			Err:     "ID field is required",
			Message: "Component ID is required",
		})
	}

	component, err := query.GetComponent(ctx, reqData.ID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{
			Err:     err.Error(),
			Message: fmt.Sprintf("Failed to get component[id=%s]", reqData.ID),
		})
	}

	logSelector := getLogSelectorByName(component.LogSelectors, reqData.Name)
	if logSelector == nil {
		return c.JSON(http.StatusBadRequest, dutyAPI.HTTPError{
			Err:     "Log selector was not found",
			Message: fmt.Sprintf("Log selector with the name '%s' was not found. Available names: [%s]", reqData.Name, strings.Join(getSelectorNames(component.LogSelectors), ", ")),
		})
	}

	templater := ctx.NewStructTemplater(component.GetAsEnvironment(), "", nil)
	if err := templater.Walk(logSelector); err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{
			Err:     err.Error(),
			Message: "failed to parse log selector templates.",
		})
	}

	apmHubPayload := map[string]any{
		"id":   component.ExternalId,
		"type": component.Type,
		// TODO: Should these be intersection or union
		"labels": collections.MergeMap(logSelector.Labels, reqData.Labels),
	}
	resp, err := makePostRequest(fmt.Sprintf("%s/%s", api.ApmHubPath, "search"), apmHubPayload)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{
			Err:     err.Error(),
			Message: "Failed to query apm-hub.",
		})
	}

	if err := c.Stream(resp.StatusCode, resp.Header.Get("Content-Type"), resp.Body); err != nil {
		return c.JSON(http.StatusInternalServerError, dutyAPI.HTTPError{
			Err:     err.Error(),
			Message: "Failed to stream response.",
		})
	}

	return nil
}

func makePostRequest(url string, data any) (*http.Response, error) {
	requestBody, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// getLogSelectorByName find the right log selector by the given name.
// If name is empty, the first log selector is returned.
func getLogSelectorByName(selectors types.LogSelectors, name string) *types.LogSelector {
	if len(selectors) == 0 {
		return nil
	}

	if name == "" {
		// if the user didn't make any selection on log selector, we will use the first one
		return &selectors[0]
	}

	for _, selector := range selectors {
		if selector.Name == name {
			return &selector
		}
	}

	return nil
}

func getSelectorNames(logSelectors types.LogSelectors) []string {
	var names = make([]string, len(logSelectors))
	for i, selector := range logSelectors {
		names[i] = selector.Name
	}

	return names
}
