package echo

import (
	"errors"
	"net/http"

	"github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/vars"
	echov4 "github.com/labstack/echo/v4"
	"github.com/ory/client-go"
	"github.com/samber/lo"
)

type UpdateUserRequest struct {
	ID string `json:"id" form:"id"`

	FirstName *string `json:"firstName" form:"firstName"`
	LastName  *string `json:"lastName" form:"lastName"`
	Email     *string `json:"email" form:"email"`
	Role      *string `json:"role" form:"role"`
	Active    *bool   `json:"active" form:"active"`
}

func (t *UpdateUserRequest) ToUpdateIdentityBody(traits map[string]any) client.UpdateIdentityBody {
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
		return api.Errorf(api.EINVALID, "updating users is only supported when using Kratos auth mode")
	}

	var req UpdateUserRequest
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

	return c.JSON(http.StatusOK, identity.Traits)
}
