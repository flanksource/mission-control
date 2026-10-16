package rbac

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"

	dutyAPI "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/kopper"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

// managedKind is a kind of authorization object managed through the API: Scope, Role or RoleBinding.
// They're only written through these handlers, which store them as the CRD reconcilers do: validated,
// with why an invalid one isn't in effect. Writes through /db are refused (see DbMiddleware).
type managedKind struct {
	// path is the plural name used in routes, e.g. role-bindings.
	path string

	// table stores the kind.
	table string

	// object is the RBAC object whole-type grants of the kind are checked on, e.g. by the shipped admin Role.
	object string

	// newObject returns an empty object to decode a request into.
	newObject func() client.Object

	// decodeRequest decodes a request into an object, when the request's shape differs from the object's. Optional.
	decodeRequest func(decoder *json.Decoder) (client.Object, error)

	// newModel returns a pointer to an empty model of the kind, and newList to an empty slice of them.
	newModel func() any
	newList  func() any

	// validate checks the object on its own, without what it references. An object that fails it can never
	// become valid, so it's rejected. Problems with what it references are stored, as the CRD reconcilers do.
	validate func(obj client.Object) error

	persist func(ctx context.Context, obj client.Object, source string, createdBy *uuid.UUID) error
	delete  func(ctx context.Context, id string) error

	// report returns fields added to the response about a stored object, beyond its model. Optional.
	report func(ctx context.Context, model any) (map[string]any, error)
}

var managedKinds = []managedKind{
	{
		path:      "scopes",
		table:     "scopes",
		object:    adapter.ObjectScopes,
		newObject: func() client.Object { return &v1.Scope{} },
		newModel:  func() any { return &models.Scope{} },
		newList:   func() any { return &[]models.Scope{} },
		validate: func(obj client.Object) error {
			return adapter.ValidateScopeTargets(obj.(*v1.Scope).Spec.Targets)
		},
		persist: func(ctx context.Context, obj client.Object, source string, createdBy *uuid.UUID) error {
			return db.PersistScope(ctx, obj.(*v1.Scope), source, createdBy)
		},
		delete: db.DeleteScope,
	},
	{
		path:          "roles",
		table:         "roles",
		object:        adapter.ObjectRoles,
		newObject:     func() client.Object { return &v1.Role{} },
		decodeRequest: decodeRoleRequest,
		newModel:      func() any { return &models.Role{} },
		newList:       func() any { return &[]models.Role{} },
		validate: func(obj client.Object) error {
			role := obj.(*v1.Role)
			return adapter.ValidateRoleSpec(role.GetName(), role.Spec.Rules)
		},
		persist: func(ctx context.Context, obj client.Object, source string, createdBy *uuid.UUID) error {
			return db.PersistRole(ctx, obj.(*v1.Role), source, createdBy)
		},
		delete: db.DeleteRole,
		report: func(ctx context.Context, model any) (map[string]any, error) {
			bindings, err := adapter.BindingsWithUnappliedRules(ctx, *model.(*models.Role))
			if err != nil {
				return nil, err
			}
			return map[string]any{"bindings_with_unapplied_rules": lo.CoalesceSliceOrEmpty(bindings)}, nil
		},
	},
	{
		path:      "role-bindings",
		table:     "role_bindings",
		object:    adapter.ObjectRoleBindings,
		newObject: func() client.Object { return &v1.RoleBinding{} },
		newModel:  func() any { return &models.RoleBinding{} },
		newList:   func() any { return &[]models.RoleBinding{} },
		validate: func(obj client.Object) error {
			return obj.(*v1.RoleBinding).Spec.Validate()
		},
		persist: func(ctx context.Context, obj client.Object, source string, createdBy *uuid.UUID) error {
			return db.PersistRoleBinding(ctx, obj.(*v1.RoleBinding), source, createdBy)
		},
		delete: db.DeleteRoleBinding,
		report: func(ctx context.Context, model any) (map[string]any, error) {
			compiled, err := adapter.ValidateBinding(ctx, nil, *model.(*models.RoleBinding))
			if err != nil && !adapter.IsValidationError(err) {
				return nil, err
			}
			return map[string]any{"unapplied_rules": lo.CoalesceSliceOrEmpty(compiled.Unapplied)}, nil
		},
	},
}

// managedTables are the tables of the managed kinds, which can't be written through /db.
var managedTables = map[string]string{}

func init() {
	for _, kind := range managedKinds {
		managedTables[kind.table] = "/rbac/" + kind.path
	}
	// A Scope's targets are stored with it, and derived from it
	managedTables["scope_targets"] = "/rbac/scopes"
}

func registerManagedKinds(e *echo.Echo) {
	for _, kind := range managedKinds {
		check := func(action string) MiddlewareFunc {
			return Authorization(kind.object, action)
		}

		group := e.Group("/rbac/" + kind.path)
		group.GET("", kind.list, check(policy.ActionRead))
		group.GET("/:namespace/:name", kind.get, check(policy.ActionRead))
		group.POST("", kind.create, check(policy.ActionCreate))
		group.PUT("/:namespace/:name", kind.update, check(policy.ActionUpdate))
		group.DELETE("/:namespace/:name", kind.remove, check(policy.ActionDelete))
	}
}

// storedObject is what the handlers need to know about a stored object.
type storedObject struct {
	ID        uuid.UUID
	Source    string
	CreatedBy *uuid.UUID
}

// find returns the stored object with the namespace and name, or nil.
func (k managedKind) find(ctx context.Context, namespace, name string) (*storedObject, error) {
	var objects []storedObject
	if err := ctx.DB().Table(k.table).Select("id, source, created_by").
		Where("namespace = ? AND name = ? AND deleted_at IS NULL", namespace, name).
		Limit(1).Find(&objects).Error; err != nil {
		return nil, ctx.Oops().Wrap(err)
	} else if len(objects) == 0 {
		return nil, nil
	}
	return &objects[0], nil
}

// errShippedNamespace is returned for any write in the shipped namespace, which only Mission Control writes to.
func errShippedNamespace() error {
	return dutyAPI.Errorf(dutyAPI.ECONFLICT, "namespace %s is reserved for objects shipped with Mission Control", adapter.ShippedNamespace)
}

func errAccessTokenNamespace() error {
	return dutyAPI.Errorf(dutyAPI.ECONFLICT, "namespace %s is reserved for the role bindings of access tokens; they're created and deleted with their token", adapter.AccessTokenNamespace)
}

// editable finds a stored object that can be changed through the API: one created through it.
// Objects from Kubernetes (or synced from it) are changed where they're defined, which would overwrite any change here.
func (k managedKind) editable(ctx context.Context, namespace, name string) (*storedObject, error) {
	if namespace == adapter.ShippedNamespace {
		return nil, errShippedNamespace()
	} else if namespace == adapter.AccessTokenNamespace {
		return nil, errAccessTokenNamespace()
	}

	stored, err := k.find(ctx, namespace, name)
	if err != nil {
		return nil, err
	} else if stored == nil {
		return nil, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "%s %s/%s not found", k.path, namespace, name)
	} else if stored.Source != models.SourceUI {
		return nil, dutyAPI.Errorf(dutyAPI.ECONFLICT, "%s %s/%s is managed by %s; change it there", k.path, namespace, name, stored.Source)
	}
	return stored, nil
}

// load returns the stored object with the id, as the kind's model.
func (k managedKind) load(ctx context.Context, id uuid.UUID) (any, error) {
	model := k.newModel()
	if err := ctx.DB().Where("id = ?", id).First(model).Error; err != nil {
		return nil, ctx.Oops().Wrap(err)
	}
	return model, nil
}

func (k managedKind) list(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	query := ctx.DB().Where("deleted_at IS NULL").Order("namespace, name")
	namespace := c.QueryParam("namespace")
	if namespace != "" {
		query = query.Where("namespace = ?", namespace)
	}
	// Access tokens' bindings grant nothing, so they're left out unless asked for, or their namespace is
	if k.table == "role_bindings" && c.QueryParam("include") != "access-tokens" && namespace != adapter.AccessTokenNamespace {
		query = query.Where("namespace != ?", adapter.AccessTokenNamespace)
	}

	list := k.newList()
	if err := query.Find(list).Error; err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}
	return c.JSON(http.StatusOK, list)
}

func (k managedKind) get(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	stored, err := k.find(ctx, c.Param("namespace"), c.Param("name"))
	if err != nil {
		return dutyAPI.WriteError(c, err)
	} else if stored == nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.ENOTFOUND, "%s %s/%s not found", k.path, c.Param("namespace"), c.Param("name")))
	}

	return k.respond(c, ctx, http.StatusOK, stored.ID)
}

func (k managedKind) create(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	obj, err := k.decode(c, "", "")
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	if obj.GetNamespace() == adapter.ShippedNamespace {
		return dutyAPI.WriteError(c, errShippedNamespace())
	} else if obj.GetNamespace() == adapter.AccessTokenNamespace {
		return dutyAPI.WriteError(c, errAccessTokenNamespace())
	}

	if stored, err := k.find(ctx, obj.GetNamespace(), obj.GetName()); err != nil {
		return dutyAPI.WriteError(c, err)
	} else if stored != nil {
		return dutyAPI.WriteError(c, dutyAPI.Errorf(dutyAPI.ECONFLICT, "%s %s/%s already exists", k.path, obj.GetNamespace(), obj.GetName()))
	}

	id := uuid.New()
	obj.SetUID(k8sTypes.UID(id.String()))
	var createdBy *uuid.UUID
	if user := ctx.User(); user != nil {
		createdBy = &user.ID
	}

	if err := k.save(ctx, obj, createdBy); err != nil {
		return dutyAPI.WriteError(c, err)
	}
	return k.respond(c, ctx, http.StatusCreated, id)
}

func (k managedKind) update(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)
	namespace, name := c.Param("namespace"), c.Param("name")

	stored, err := k.editable(ctx, namespace, name)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	obj, err := k.decode(c, namespace, name)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	obj.SetUID(k8sTypes.UID(stored.ID.String()))
	if err := k.save(ctx, obj, stored.CreatedBy); err != nil {
		return dutyAPI.WriteError(c, err)
	}
	return k.respond(c, ctx, http.StatusOK, stored.ID)
}

func (k managedKind) remove(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)

	stored, err := k.editable(ctx, c.Param("namespace"), c.Param("name"))
	if err != nil {
		return dutyAPI.WriteError(c, err)
	}

	if err := k.delete(ctx, stored.ID.String()); err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}
	return c.JSON(http.StatusOK, dutyAPI.HTTPSuccess{Message: fmt.Sprintf("%s %s/%s deleted", k.path, c.Param("namespace"), c.Param("name"))})
}

// decode reads the object from the request body, in the same shape as the CRD, and rejects it when it's invalid on its own.
// Unknown fields are rejected rather than ignored. When namespace and name are given (from the route),
// the body must match them or leave them out.
func (k managedKind) decode(c echo.Context, namespace, name string) (client.Object, error) {
	decoder := json.NewDecoder(c.Request().Body)
	decoder.DisallowUnknownFields()

	var obj client.Object
	var err error
	if k.decodeRequest != nil {
		obj, err = k.decodeRequest(decoder)
	} else {
		obj = k.newObject()
		err = decoder.Decode(obj)
	}
	if err != nil {
		return nil, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid request body: %v", err)
	}

	if namespace != "" {
		if obj.GetNamespace() != "" && obj.GetNamespace() != namespace || obj.GetName() != "" && obj.GetName() != name {
			return nil, dutyAPI.Errorf(dutyAPI.EINVALID, "metadata.namespace and metadata.name can't be changed")
		}
		obj.SetNamespace(namespace)
		obj.SetName(name)
	}

	if errs := validation.IsDNS1123Label(obj.GetNamespace()); len(errs) > 0 {
		return nil, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid metadata.namespace %q: %v", obj.GetNamespace(), errs)
	}
	if errs := validation.IsDNS1123Subdomain(obj.GetName()); len(errs) > 0 {
		return nil, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid metadata.name %q: %v", obj.GetName(), errs)
	}

	if err := k.validate(obj); err != nil {
		return nil, dutyAPI.Errorf(dutyAPI.EINVALID, "invalid %s %s/%s: %v", k.path, obj.GetNamespace(), obj.GetName(), err)
	}

	return obj, nil
}

// save stores the object. An object that's invalid because of what it references, e.g. a missing Scope, is stored too,
// with why it isn't in effect, so it isn't an error here: the response carries the object's error and error_reason.
func (k managedKind) save(ctx context.Context, obj client.Object, createdBy *uuid.UUID) error {
	err := k.persist(ctx, obj, models.SourceUI, createdBy)
	var notReady *kopper.NotReadyError
	if err != nil && !errors.As(err, &notReady) {
		return ctx.Oops().Wrap(err)
	}
	return nil
}

func (k managedKind) respond(c echo.Context, ctx context.Context, status int, id uuid.UUID) error {
	model, err := k.load(ctx, id)
	if err != nil {
		return dutyAPI.WriteError(c, err)
	} else if k.report == nil {
		return c.JSON(status, model)
	}

	report, err := k.report(ctx, model)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}

	raw, err := json.Marshal(model)
	if err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return dutyAPI.WriteError(c, ctx.Oops().Wrap(err))
	}
	maps.Copy(body, report)
	return c.JSON(status, body)
}

// roleRequest is a Role as the API takes it. Its rules have no deny: rules only allow.
type roleRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec struct {
		Description string            `json:"description,omitempty"`
		Rules       []roleRequestRule `json:"rules"`
	} `json:"spec,omitempty"`
	Status v1.RoleStatus `json:"status,omitempty"`
}

type roleRequestRule struct {
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Action      string             `json:"action"`
	Resource    *v1.ScopeReference `json:"resource,omitempty"`
	Target      *v1.ScopeReference `json:"target,omitempty"`
}

func decodeRoleRequest(decoder *json.Decoder) (client.Object, error) {
	var request roleRequest
	if err := decoder.Decode(&request); err != nil {
		return nil, err
	}

	role := &v1.Role{TypeMeta: request.TypeMeta, ObjectMeta: request.ObjectMeta, Status: request.Status}
	role.Spec.Description = request.Spec.Description
	for _, rule := range request.Spec.Rules {
		role.Spec.Rules = append(role.Spec.Rules, v1.RoleRule{
			Name:        rule.Name,
			Description: rule.Description,
			Action:      rule.Action,
			Resource:    rule.Resource,
			Target:      rule.Target,
		})
	}
	return role, nil
}
