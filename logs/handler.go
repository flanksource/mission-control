package logs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/commons/template"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/labstack/echo/v4"
)

func LogsHandler(c echo.Context) error {
	var reqData struct {
		ID     string            `json:"id"`
		Name   string            `json:"name"`
		Labels map[string]string `json:"labels"`
	}
	if err := c.Bind(&reqData); err != nil {
		return c.JSON(http.StatusBadRequest, api.HTTPError{
			Error:   err.Error(),
			Message: "Invalid request body",
		})
	}

	if reqData.ID == "" {
		return c.JSON(http.StatusBadRequest, api.HTTPError{
			Error:   "ID field is required",
			Message: "Component ID is required",
		})
	}

	component, err := duty.GetComponent(c.Request().Context(), db.Gorm, reqData.ID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{
			Error:   err.Error(),
			Message: fmt.Sprintf("Failed to get component[id=%s]", reqData.ID),
		})
	}

	logSelector := getLogSelectorByName(component.LogSelectors, reqData.Name)
	if logSelector == nil {
		return c.JSON(http.StatusBadRequest, api.HTTPError{
			Error:   "Log selector was not found",
			Message: fmt.Sprintf("Log selector with the name '%s' was not found. Available names: [%s]", reqData.Name, strings.Join(getSelectorNames(component.LogSelectors), ", ")),
		})
	}

	templater := template.StructTemplater{
		Values:         component.GetAsEnvironment(),
		ValueFunctions: true,
		DelimSets: []template.Delims{
			{Left: "{{", Right: "}}"},
			{Left: "$(", Right: ")"},
		},
	}
	if err := templater.Walk(logSelector); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{
			Error:   err.Error(),
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
		return c.JSON(http.StatusInternalServerError, api.HTTPError{
			Error:   err.Error(),
			Message: "Failed to query apm-hub.",
		})
	}

	if err := c.Stream(resp.StatusCode, resp.Header.Get("Content-Type"), resp.Body); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{
			Error:   err.Error(),
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
