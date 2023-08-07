package agent

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
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

	var (
		username = fmt.Sprintf("agent-%s", generateRandomString(8))
		password = generateRandomString(32)
	)

	// TODO: Only if unauthenticated, we need to create a user
	id, err := db.CreatePerson(ctx, username, password)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{Error: err.Error(), Message: "error generating agent"})
	}

	if ok, err := rbac.Enforcer.AddRoleForUser(id.String(), "agent"); !ok {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{Error: err.Error(), Message: "error generating agent"})
	} else if err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{Error: err.Error(), Message: "error generating agent"})
	}

	if err := db.CreateAgent(ctx, body.Name, &id, body.Properties); err != nil {
		return c.JSON(http.StatusInternalServerError, api.HTTPError{Error: err.Error(), Message: "error generating agent"})
	}

	return c.JSON(http.StatusCreated, api.GeneratedAgent{
		ID:       id.String(),
		Username: username,
		Password: password,
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
