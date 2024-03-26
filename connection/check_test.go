package connection_test

import (
	gocontext "context"
	"testing"

	commons "github.com/flanksource/commons/context"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/incident-commander/connection"
)

// TODO: Remove this test
func TestConnection(t *testing.T) {
	m := models.Connection{
		Type:     models.ConnectionTypeTelegram,
		Password: "whatever",
	}

	ctx := context.Context{Context: commons.NewContext(gocontext.Background())}
	err := connection.Test(ctx, &m)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
}
