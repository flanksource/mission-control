package echo

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/types"
	echov4 "github.com/labstack/echo/v4"
	"github.com/ory/client-go"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/vars"
)

type UpdatePersonRequest struct {
	ID string `form:"id"`

	FirstName *string `form:"firstName"`
	LastName  *string `form:"lastName"`
	Email     *string `form:"email"`
	Role      *string `form:"role"`
	Active    *bool   `form:"active"`
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
		out.State = lo.Ternary(*t.Active, client.IDENTITYSTATE_ACTIVE, client.IDENTITYSTATE_INACTIVE)
	}

	return out
}

type PersonController struct {
	kratos *client.APIClient
}

func (t *PersonController) UpdatePerson(c echov4.Context) error {
	ctx := c.Request().Context().(context.Context)

	if vars.AuthMode != auth.Kratos {
		return api.Errorf(api.EINVALID, "updating person is only supported when using Kratos auth mode")
	}

	var req UpdatePersonRequest
	if err := c.Bind(&req); err != nil {
		return api.Errorf(api.EINVALID, "invalid request body: %v", err)
	}

	var traits types.JSONMap
	err := ctx.DB().Table("identities").Select("traits").Where("id = ?", req.ID).Scan(&traits).Error
	if err != nil {
		return err
	} else if traits == nil {
		return api.WriteError(c, api.Errorf(api.ENOTFOUND, "person %s not found", req.ID))
	}

	updateRequest := t.kratos.IdentityApi.UpdateIdentity(ctx, req.ID).UpdateIdentityBody(req.ToUpdateIdentityBody(traits))
	identity, _, err := updateRequest.Execute()
	if err != nil {
		var clientErr *client.GenericOpenAPIError
		if errors.As(err, &clientErr) {
			return c.String(http.StatusBadRequest, string(clientErr.Body()))
		}

		return err
	}

	if req.Role != nil {
		if err := rbac.DeleteAllRolesForUser(req.ID); err != nil {
			return api.WriteError(c, fmt.Errorf("failed to delete existing roles: %w", err))
		}

		if err := rbac.AddRoleForUser(req.ID, *req.Role); err != nil {
			return api.WriteError(c, fmt.Errorf("failed to add the new role: %w", err))
		}
	}

	return c.JSON(http.StatusOK, identity.Traits)
}

func (t *PersonController) DeletePerson(c echov4.Context) error {
	ctx := c.Request().Context().(context.Context)

	if vars.AuthMode != auth.Kratos {
		return api.Errorf(api.EINVALID, "deleting a person is only supported when using Kratos auth mode")
	}

	id := c.Param("id")
	response, err := t.kratos.IdentityApi.DeleteIdentity(ctx, id).Execute()
	if err != nil {
		var clientErr *client.GenericOpenAPIError
		if errors.As(err, &clientErr) {
			return c.String(http.StatusBadRequest, string(clientErr.Body()))
		}

		return err
	}

	return c.Stream(response.StatusCode, "application/json", response.Body)
}
