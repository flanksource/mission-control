package echo

import (
	"errors"
	"net/http"
	"strings"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	echov4 "github.com/labstack/echo/v4"
	"github.com/ory/client-go"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
	icrbac "github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/vars"
)

type UpdatePersonRequest struct {
	ID string `form:"id"`

	FirstName *string `form:"firstName"`
	LastName  *string `form:"lastName"`
	Email     *string `form:"email"`

	// Deprecated: built-in roles were removed, so a request that sets a role is rejected.
	Role   *string `form:"role"`
	Active *bool   `form:"active"`
}

func (t *UpdatePersonRequest) ToUpdateIdentityBody(traits map[string]any) client.UpdateIdentityBody {
	out := client.UpdateIdentityBody{
		Traits: traits,
	}

	{
		name := map[string]any{}
		if traitName, ok := traits["name"]; ok {
			name = traitName.(map[string]any)
		}

		if t.FirstName != nil {
			name["first"] = *t.FirstName
		}

		if t.LastName != nil {
			name["last"] = *t.LastName
		}

		if len(name) != 0 {
			out.Traits["name"] = name
		}
	}

	if t.Email != nil {
		out.Traits["email"] = *t.Email
	}

	if t.Active != nil {
		out.State = lo.Ternary(*t.Active, auth.IdentityStateActive, auth.IdentityStateInactive)
	}

	return out
}

type PersonController struct {
	kratos *client.APIClient
}

// RegisterPeopleRoutes registers the person operations, which create, change or delete a person's account,
// and the routes of the signed-in person's analytics.
func RegisterPeopleRoutes(e *echov4.Echo, kratos *client.APIClient) {
	controller := PersonController{kratos: kratos}
	e.POST("/people", CreatePerson, icrbac.PersonManage())
	e.POST("/people/update", controller.UpdatePerson, icrbac.PersonManage())
	e.DELETE("/people/:id", controller.DeletePerson, icrbac.PersonManage())
	e.GET("/people/analytics", ListAnalytics)
	e.POST("/people/analytics", RecordAnalytic)
}

type CreatePersonRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// CreatePerson creates a human. Only under basic authentication, where signing in creates no one: Kratos creates a
// person when an invite is accepted, and Clerk on their first sign-in.
func CreatePerson(c echov4.Context) error {
	ctx := c.Request().Context().(context.Context)

	if vars.AuthMode != auth.Basic {
		return api.WriteError(c, api.Errorf(api.EINVALID, "people are only created this way under basic authentication"))
	}

	var req CreatePersonRequest
	if err := c.Bind(&req); err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid request body: %v", err))
	}
	req.Name, req.Email = strings.TrimSpace(req.Name), strings.TrimSpace(req.Email)
	if req.Name == "" || req.Email == "" {
		return api.WriteError(c, api.Errorf(api.EINVALID, "name and email are required"))
	}

	var taken int64
	if err := ctx.DB().Model(&models.Person{}).Where("LOWER(email) = LOWER(?) AND deleted_at IS NULL", req.Email).Count(&taken).Error; err != nil {
		return api.WriteError(c, ctx.Oops().Wrap(err))
	} else if taken > 0 {
		return api.WriteError(c, api.Errorf(api.ECONFLICT, "a person with email %s already exists", req.Email))
	}

	person, err := db.CreatePerson(ctx, req.Name, req.Email, "")
	if err != nil {
		return api.WriteError(c, err)
	}
	return c.JSON(http.StatusCreated, person)
}

// kratosIdentityTraits returns the traits of the person's Kratos identity, or nil when they have none.
func (t *PersonController) kratosIdentityTraits(ctx context.Context, id string) (types.JSONMap, error) {
	if vars.AuthMode != auth.Kratos {
		return nil, nil
	}
	var traits types.JSONMap
	err := ctx.DB().Table("identities").Select("traits").Where("id = ?", id).Scan(&traits).Error
	return traits, err
}

// UpdatePerson changes a person's name or email: through their Kratos identity when they have one, on the row
// otherwise.
func (t *PersonController) UpdatePerson(c echov4.Context) error {
	ctx := c.Request().Context().(context.Context)

	var req UpdatePersonRequest
	if err := c.Bind(&req); err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid request body: %v", err))
	}

	if req.Role != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "a person can't be given a role: built-in roles were removed, add them to teams instead"))
	}

	person, err := db.FindPersonForOperation(ctx, req.ID)
	if err != nil {
		return api.WriteError(c, err)
	}

	traits, err := t.kratosIdentityTraits(ctx, req.ID)
	if err != nil {
		return api.WriteError(c, ctx.Oops().Wrap(err))
	}

	if traits == nil {
		if req.Active != nil {
			return api.WriteError(c, api.Errorf(api.EINVALID, "only a person with a Kratos identity can be disabled or re-enabled"))
		}

		var name *string
		if req.FirstName != nil || req.LastName != nil {
			name = lo.ToPtr(strings.TrimSpace(lo.FromPtr(req.FirstName) + " " + lo.FromPtr(req.LastName)))
		}
		if err := db.UpdatePersonRow(ctx, person.ID, name, req.Email); err != nil {
			return api.WriteError(c, ctx.Oops().Wrap(err))
		}
		updated, err := db.FindPersonForOperation(ctx, req.ID)
		if err != nil {
			return api.WriteError(c, err)
		}
		return c.JSON(http.StatusOK, updated)
	}

	updateRequest := t.kratos.IdentityAPI.UpdateIdentity(ctx, req.ID).UpdateIdentityBody(req.ToUpdateIdentityBody(traits))
	identity, _, err := updateRequest.Execute()
	if err != nil {
		var clientErr *client.GenericOpenAPIError
		if errors.As(err, &clientErr) {
			return c.String(http.StatusBadRequest, string(clientErr.Body()))
		}

		return api.WriteError(c, ctx.Oops().Wrap(err))
	}

	return c.JSON(http.StatusOK, identity.Traits)
}

// DeletePerson deletes a person: through their Kratos identity when they have one, soft-deleting the row otherwise.
func (t *PersonController) DeletePerson(c echov4.Context) error {
	ctx := c.Request().Context().(context.Context)

	id := c.Param("id")
	person, err := db.FindPersonForOperation(ctx, id)
	if err != nil {
		return api.WriteError(c, err)
	}

	traits, err := t.kratosIdentityTraits(ctx, id)
	if err != nil {
		return api.WriteError(c, ctx.Oops().Wrap(err))
	}

	if traits == nil {
		if err := db.SoftDeletePerson(ctx, person.ID); err != nil {
			return api.WriteError(c, ctx.Oops().Wrap(err))
		}
		return c.NoContent(http.StatusNoContent)
	}

	response, err := t.kratos.IdentityAPI.DeleteIdentity(ctx, id).Execute()
	if err != nil {
		var clientErr *client.GenericOpenAPIError
		if errors.As(err, &clientErr) {
			return c.String(http.StatusBadRequest, string(clientErr.Body()))
		}

		return api.WriteError(c, ctx.Oops().Wrap(err))
	}

	return c.Stream(response.StatusCode, "application/json", response.Body)
}

type RecordAnalyticRequest struct {
	Key string `json:"key"`
}

// ListAnalytics lists the actions the signed-in person has taken. A person reads their own without a grant.
func ListAnalytics(c echov4.Context) error {
	ctx := c.Request().Context().(context.Context)
	user := ctx.User()
	if user == nil {
		return api.WriteError(c, api.Errorf(api.EUNAUTHORIZED, "error fetching user"))
	}

	analytics, err := db.ListPersonAnalytics(ctx, user.ID)
	if err != nil {
		return api.WriteError(c, ctx.Oops().Wrapf(err, "unable to list analytics"))
	}
	return c.JSON(http.StatusOK, api.HTTPSuccess{Message: "success", Payload: analytics})
}

// RecordAnalytic records an action the signed-in person took. A person records their own without a grant,
// and only their own: the person is the signed-in one, not one the request names.
func RecordAnalytic(c echov4.Context) error {
	ctx := c.Request().Context().(context.Context)
	user := ctx.User()
	if user == nil {
		return api.WriteError(c, api.Errorf(api.EUNAUTHORIZED, "error fetching user"))
	}

	var req RecordAnalyticRequest
	if err := c.Bind(&req); err != nil {
		return api.WriteError(c, api.Errorf(api.EINVALID, "invalid request body: %v", err))
	}
	if req.Key == "" {
		return api.WriteError(c, api.Errorf(api.EINVALID, "key is required"))
	}

	if err := db.RecordPersonAnalytic(ctx, user.ID, req.Key); err != nil {
		return api.WriteError(c, ctx.Oops().Wrapf(err, "unable to record analytic"))
	}
	return c.NoContent(http.StatusNoContent)
}
