package agent

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"net/http"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac"
	"github.com/labstack/echo/v4"
)

func GenerateAgent(c echo.Context) error {
	ctx := c.(*api.Context)

	var body api.GenerateAgentRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&body); err != nil {
		return c.JSON(http.StatusBadRequest, api.HTTPError{Error: err.Error()})
	}

	req := db.CreateUserRequest{
		Username:   generateRandomString(10),
		Password:   generateRandomString(32),
		Properties: body.Properties,
	}

	id, err := db.CreatePerson(ctx, req)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{Error: err.Error(), Message: "error generating agent"})
	}

	if ok, err := rbac.Enforcer.AddRoleForUser(id, "agent"); !ok {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{Error: err.Error(), Message: "error generating agent"})
	} else if err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{Error: err.Error(), Message: "error generating agent"})
	}

	if _, err := db.GetOrCreateAgent(ctx, body.Name); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{Error: err.Error(), Message: "error generating agent"})
	}

	return c.JSON(http.StatusCreated, api.GeneratedAgent{
		ID:       id,
		Username: req.Username,
		Password: req.Password,
	})
}

// generateRandomString generates a random alphanumeric string of the given length.
func generateRandomString(length int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)
	for i := range result {
		val, err := generateRandomInt(len(letters))
		if err != nil {
			panic(err) // Handle error in a way that suits your needs
		}
		result[i] = letters[val]
	}
	return string(result)
}

// generateRandomInt generates a random integer up to max.
func generateRandomInt(max int) (int, error) {
	var n uint32
	err := binary.Read(rand.Reader, binary.LittleEndian, &n)
	if err != nil {
		return 0, err
	}
	return int(n) % max, nil
}
