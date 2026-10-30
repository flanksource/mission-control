package canary

import (
	"io"
	"net/url"

	"github.com/flanksource/commons/http"
	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/flanksource/incident-commander/api"
	echoSrv "github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/rbac"
)

func init() {
	echoSrv.RegisterRoutes(RegisterRoutes)
}

func RegisterRoutes(e *echo.Echo) {
	// Registered ahead of the /canary forward, so a run is authorized here for every entry point
	e.POST("/canary/run/check/:id", RunCheckHandler)
}

// RunCheck runs a check in canary-checker now, as the caller, and returns canary-checker's response.
// It requires create on canaries. The caller must close the response's body.
func RunCheck(ctx context.Context, checkID string) (*http.Response, error) {
	if !rbac.CheckContext(ctx, policy.ObjectCanary, policy.ActionCreate) {
		return nil, ctx.Oops().Code(dutyAPI.EFORBIDDEN).Errorf("forbidden: %s on %s is required to run a check", policy.ActionCreate, policy.ObjectCanary)
	}

	if _, err := uuid.Parse(checkID); err != nil {
		return nil, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid check id %q", checkID)
	}

	endpoint, err := url.JoinPath(api.CanaryCheckerPath, "run", "check", checkID)
	if err != nil {
		return nil, ctx.Oops().Wrap(err)
	}

	resp, err := http.NewClient().R(ctx).Post(endpoint, nil)
	if err != nil {
		return nil, ctx.Oops().Wrapf(err, "failed to run check %s", checkID)
	}
	return resp, nil
}

// RunCheckHandler runs a check and replies with canary-checker's response.
func RunCheckHandler(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	resp, err := RunCheck(ctx, c.Param("id"))
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}
	return c.Blob(resp.StatusCode, resp.Header.Get(echo.HeaderContentType), body)
}
