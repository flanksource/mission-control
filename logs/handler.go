package logs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/duty"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/kommons/ktemplate"
	"github.com/labstack/echo/v4"
)

func LogsHandler(c echo.Context) error {
	req := c.Request()

	var form = make(map[string]any)
	if err := c.Bind(&form); err != nil {
		return c.JSON(http.StatusBadRequest, api.HTTPError{
			Error:   err.Error(),
			Message: "Invalid request body",
		})
	}

	componentID, ok := form["id"].(string)
	if !ok {
		return c.JSON(http.StatusBadRequest, api.HTTPError{
			Error:   "'id' field is required",
			Message: "component id is required",
		})
	}

	component, err := duty.GetComponent(req.Context(), db.Gorm, componentID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{
			Error:   err.Error(),
			Message: "failed to get component by the given ID",
		})
	}

	selectorName, _ := form["name"].(string)
	logSelector := getLabelsFromLogSelectors(component.LogSelectors, selectorName)
	if logSelector == nil {
		return c.JSON(http.StatusBadRequest, api.HTTPError{
			Error:   "log selector was not found",
			Message: fmt.Sprintf("log selector with the name '%s' was not found.", selectorName),
		})
	}

	templater := ktemplate.StructTemplater{
		Values:         component.GetAsEnvironment(),
		ValueFunctions: true,
		DelimSets: []ktemplate.Delims{
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

	modifiedForm := injectLabelsToForm(logSelector.Labels, form)
	resp, err := makePostRequest(fmt.Sprintf("%s/%s", api.ApmHubPath, "search"), modifiedForm)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{
			Error:   err.Error(),
			Message: "failed to query apm-hub.",
		})
	}

	c.Stream(resp.StatusCode, resp.Header.Get("Content-Type"), resp.Body)
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
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// getLabelsFromLogSelectors returns a map of labels from the logs selectors
func getLabelsFromLogSelectors(selectors models.LogSelectors, name string) *models.LogSelector {
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

	logger.Debugf("could not find log selector with name %s", name)
	return nil
}

func injectLabelsToForm(injectLabels map[string]string, form map[string]any) map[string]any {
	// Make sure label exists so we can inject our labels
	if _, ok := form["labels"]; !ok {
		form["labels"] = make(map[string]any)
	}

	if labels, ok := form["labels"].(map[string]any); ok {
		for k, v := range injectLabels {
			labels[k] = v
		}
		form["labels"] = labels
	}

	return form
}
