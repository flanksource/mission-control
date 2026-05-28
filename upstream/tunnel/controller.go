package tunnel

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/hashicorp/yamux"
	"github.com/labstack/echo/v4"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
)

const SessionCreateHTTPEndpoint = "yamux-create-session"

func YamuxSessionCreate(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)
	agent := ctx.Agent()
	if agent == nil || agent.ID == uuid.Nil {
		return dutyAPI.WriteError(c, ctx.Oops().
			Code(dutyAPI.EUNAUTHORIZED).
			Errorf("authenticated agent is required"))
	}

	hijacker, ok := c.Response().Writer.(http.Hijacker)
	if !ok {
		return dutyAPI.WriteError(c, ctx.Oops().
			Code(dutyAPI.EINTERNAL).
			Errorf("response writer does not support hijacking"))
	}

	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}

	if _, err := rw.WriteString(
		"HTTP/1.1 101 Switching Protocols\r\n" +
			"Connection: Upgrade\r\n" +
			"Upgrade: yamux\r\n\r\n",
	); err != nil {
		_ = conn.Close()
		return nil
	}
	if err := rw.Flush(); err != nil {
		_ = conn.Close()
		return nil
	}

	session, err := yamux.Server(conn, nil)
	if err != nil {
		_ = conn.Close()
		return err
	}

	defaultManager.Register(agent.ID, session)

	go func() {
		<-session.CloseChan()
		defaultManager.DeleteIfSame(agent.ID, session)
	}()

	return nil
}
