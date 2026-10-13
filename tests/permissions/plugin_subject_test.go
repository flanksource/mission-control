package permissions_test

import (
	gocontext "context"

	dutyContext "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/plugin"
	pluginAPI "github.com/flanksource/incident-commander/plugin/api"
	"github.com/flanksource/incident-commander/plugin/machinery"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

var _ = Describe("Plugin host callbacks", Ordered, func() {
	const pluginNamespace = "plugin-subject"

	var (
		admin    *models.Person
		config   models.ConfigItem
		pluginID uuid.UUID
		otherID  uuid.UUID
		svc      *machinery.Service
	)

	// callback makes a host callback as the plugin, through the host's interceptor, during an operation the person
	// invoked. It returns the subject the callback was checked as.
	callback := func(person *models.Person, method string, req any, handler grpc.UnaryHandler) (string, any, error) {
		GinkgoHelper()
		token, err := plugin.MintInvocationToken(person.ID.String(), person.ID.String(), pluginID, 0)
		Expect(err).ToNot(HaveOccurred())

		ctx := metadata.NewIncomingContext(gocontext.Background(), metadata.Pairs(pluginAPI.InvocationTokenGRPCMetadataKey, token))

		var subject string
		resp, err := svc.UnaryServerInterceptor()(ctx, req, &grpc.UnaryServerInfo{FullMethod: method}, func(ctx gocontext.Context, req any) (any, error) {
			subject = ctx.(dutyContext.Context).Subject()
			return handler(ctx, req)
		})
		return subject, resp, err
	}

	getConfigItem := func(person *models.Person) (string, error) {
		subject, _, err := callback(person, pluginAPI.HostService_GetConfigItem_FullMethodName, &pluginAPI.GetConfigItemRequest{Id: config.ID.String()},
			func(ctx gocontext.Context, req any) (any, error) {
				return svc.GetConfigItem(ctx, req.(*pluginAPI.GetConfigItemRequest))
			})
		return subject, err
	}

	listConfigs := func(person *models.Person) (*pluginAPI.ConfigItemList, error) {
		_, resp, err := callback(person, pluginAPI.HostService_ListConfigs_FullMethodName,
			&pluginAPI.ListConfigsRequest{Selector: &pluginAPI.ResourceSelector{Id: config.ID.String(), Cache: "no-cache"}},
			func(ctx gocontext.Context, req any) (any, error) {
				return svc.ListConfigs(ctx, req.(*pluginAPI.ListConfigsRequest))
			})
		if err != nil {
			return nil, err
		}
		return resp.(*pluginAPI.ConfigItemList), nil
	}

	invokePlugin := func(person *models.Person) error {
		_, _, err := callback(person, pluginAPI.HostService_InvokePlugin_FullMethodName,
			&pluginAPI.InvokePluginRequest{Plugin: pluginNamespace + "/other", Operation: "describe", ConfigItemId: config.ID.String()},
			func(ctx gocontext.Context, req any) (any, error) {
				return svc.InvokePlugin(ctx, req.(*pluginAPI.InvokePluginRequest))
			})
		return err
	}

	BeforeAll(func() {
		Expect(rbac.Init(DefaultContext, []string{"admin"}, adapter.NewPermissionAdapter)).To(Succeed())
		admin = setup.CreateUserWithRole(DefaultContext, "Plugin Subject Admin", "plugin-subject-admin@test.com", policy.RoleAdmin)

		config = models.ConfigItem{ID: uuid.New(), Name: lo.ToPtr("plugin-subject"), Type: lo.ToPtr("Plugin::Test"), ConfigClass: "Test"}
		Expect(DefaultContext.DB().Create(&config).Error).To(Succeed())

		pluginID, otherID = uuid.New(), uuid.New()
		for id, name := range map[uuid.UUID]string{pluginID: "logs", otherID: "other"} {
			_, err := plugin.DefaultRegistry.Upsert(id, pluginNamespace, name, v1.PluginSpec{Source: "test"})
			Expect(err).ToNot(HaveOccurred())
			Expect(plugin.DefaultRegistry.SetManifest(id, &pluginAPI.PluginManifest{
				Name:       name,
				Operations: []*pluginAPI.OperationDef{{Name: "describe"}},
			})).To(Succeed())
		}

		svc = machinery.NewGRPCService(DefaultContext)
	})

	AfterAll(func() {
		plugin.DefaultRegistry.Remove(pluginID)
		plugin.DefaultRegistry.Remove(otherID)
		Expect(DefaultContext.DB().Delete(&config).Error).To(Succeed())
		Expect(DefaultContext.DB().Delete(admin).Error).To(Succeed())
	})

	It("checks a host callback as the plugin even when an admin invokes it", func() {
		Expect(rbac.HasPermission(DefaultContext, admin.ID.String(), &models.ABACAttribute{Config: config}, policy.ActionRead)).
			To(BeTrue(), "the admin may read the config themselves")

		subject, err := getConfigItem(admin)
		Expect(subject).To(Equal(v1.PluginSubject(pluginNamespace, "logs")))
		Expect(status.Code(err)).To(Equal(codes.PermissionDenied), "%v", err)

		list, err := listConfigs(admin)
		Expect(err).ToNot(HaveOccurred())
		Expect(list.Items).To(BeEmpty(), "rows are filtered by the plugin's grants, not the admin's")
	})

	It("refuses a plugin with no binding, with no retry as its caller", func() {
		subject := v1.PluginSubject(pluginNamespace, "logs")
		for _, action := range []string{policy.ActionRead, adapter.ActionConnectionUse} {
			Expect(rbac.Check(DefaultContext, subject, policy.ObjectConnection, action)).To(BeFalse(), action)
		}

		err := invokePlugin(admin)
		Expect(status.Code(err)).To(Equal(codes.PermissionDenied), "%v", err)
		Expect(err.Error()).To(ContainSubstring("not allowed to read config"))
	})
})
