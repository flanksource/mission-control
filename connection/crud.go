package connection

import (
	"net/http"
	"time"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac"
)

// GetConnection returns a connection with its credentials masked. It requires read on connections.
func GetConnection(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	conn, err := findConnection(ctx, c.Param("id"))
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	if err := rbac.AuthorizeConnection(ctx, *conn, policy.ActionRead); err != nil {
		return dutyAPI.WriteError(c, err)
	}

	return c.JSON(http.StatusOK, db.MaskConnection(*conn))
}

// CreateConnection creates a connection. It requires create on connections.
func CreateConnection(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	var conn models.Connection
	if err := c.Bind(&conn); err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid request body: %v", err))
	}

	created, err := createConnection(ctx, conn)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	return c.JSON(http.StatusCreated, db.MaskConnection(*created))
}

// SaveConnection updates the connection with the id, or creates it when there's none.
// Updating requires update on connections, and creating requires create.
// A credential sent back masked, as reading the connection shows it, is left unchanged.
func SaveConnection(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid connection id %q", c.Param("id")))
	}

	var conn models.Connection
	if err := c.Bind(&conn); err != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid request body: %v", err))
	}
	conn.ID = id

	stored, err := db.GetConnection(ctx, id)
	if dutyAPI.ErrorCode(err) == dutyAPI.ENOTFOUND {
		if !rbac.CheckContext(ctx, policy.ObjectConnection, policy.ActionCreate) {
			return dutyAPI.WriteError(c, ctx.Oops().Code(dutyAPI.EFORBIDDEN).
				Errorf("access denied to %s: %s required on connections", ctx.Subject(), policy.ActionCreate))
		}

		created, err := createConnection(ctx, conn)
		if err != nil {
			return dutyAPI.WriteError(c, err)
		}
		return c.JSON(http.StatusCreated, db.MaskConnection(*created))
	} else if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	if err := rbac.AuthorizeConnection(ctx, *stored, policy.ActionUpdate); err != nil {
		return dutyAPI.WriteError(c, err)
	}

	if err := validateConnection(conn); err != nil {
		return dutyAPI.WriteError(c, err)
	}

	conn = db.UnmaskConnection(conn, *stored)
	conn.Source = stored.Source
	conn.CreatedAt = stored.CreatedAt
	conn.CreatedBy = stored.CreatedBy
	conn.UpdatedAt = time.Now()
	if err := ctx.DB().Save(&conn).Error; err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "failed to update connection %s", id))
	}

	return c.JSON(http.StatusOK, db.MaskConnection(conn))
}

// DeleteConnection deletes a connection. It requires delete on connections.
func DeleteConnection(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	conn, err := findConnection(ctx, c.Param("id"))
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	if err := rbac.AuthorizeConnection(ctx, *conn, policy.ActionDelete); err != nil {
		return dutyAPI.WriteError(c, err)
	}

	if err := db.DeleteConnection(ctx, conn.ID.String()); err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrapf(err, "failed to delete connection %s", conn.ID))
	}

	return c.NoContent(http.StatusNoContent)
}

func findConnection(ctx context.Context, rawID string) (*models.Connection, error) {
	id, err := uuid.Parse(rawID)
	if err != nil {
		return nil, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid connection id %q", rawID)
	}
	return db.GetConnection(ctx, id)
}

func createConnection(ctx context.Context, conn models.Connection) (*models.Connection, error) {
	if err := validateConnection(conn); err != nil {
		return nil, err
	}

	if conn.ID == uuid.Nil {
		conn.ID = uuid.New()
	}
	conn.Source = models.SourceUI
	conn.CreatedAt = time.Time{}
	conn.UpdatedAt = time.Time{}
	conn.CreatedBy = nil
	if user := ctx.User(); user != nil {
		conn.CreatedBy = &user.ID
	}

	if err := ctx.DB().Create(&conn).Error; err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to create connection %s/%s", conn.Namespace, conn.Name)
	}
	return &conn, nil
}

func validateConnection(conn models.Connection) error {
	if conn.Name == "" {
		return dutyAPI.Errorf(dutyAPI.EINVALID, "name is required")
	}
	if conn.Type == "" {
		return dutyAPI.Errorf(dutyAPI.EINVALID, "type is required")
	}
	return nil
}
