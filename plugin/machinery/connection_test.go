package machinery

import (
	gocontext "context"
	"fmt"

	icrbac "github.com/flanksource/incident-commander/rbac"

	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/plugin"
	pluginAPI "github.com/flanksource/incident-commander/plugin/api"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

var _ = ginkgo.Describe("Plugin connection lookups", ginkgo.Ordered, func() {
	const (
		namespace = "plugin-connections-test"
		password  = "plugin-connection-password"
	)

	var (
		svc                    *Service
		conn                   models.Connection
		scraper, inlineScraper models.ConfigScraper
		config, inlineConfig   models.ConfigItem
		unbound, reader, user  *plugin.Entry
		connectionRef          = fmt.Sprintf("connection://%s/plugin-connection", namespace)
		uid                    = func() k8sTypes.UID { return k8sTypes.UID(uuid.NewString()) }
	)

	// register stores a plugin, as its CRD would, and registers it the way the reconciler does
	register := func(name string) *plugin.Entry {
		ginkgo.GinkgoHelper()
		stored := models.Plugin{ID: uuid.New(), Name: name, Namespace: namespace, Source: models.SourceCRD, Spec: []byte(`{}`)}
		Expect(DefaultContext.DB().Create(&stored).Error).To(Succeed())
		entry, err := plugin.DefaultRegistry.Upsert(stored.ID, namespace, name, v1.PluginSpec{
			Connections: v1.PluginConnectionMappings{
				Types:  map[string]string{"postgres": connectionRef},
				Labels: map[string]string{"primary": connectionRef},
			},
		})
		Expect(err).ToNot(HaveOccurred())
		return entry
	}

	bind := func(name, action, scope string, entry *plugin.Entry) {
		ginkgo.GinkgoHelper()
		Expect(db.PersistRoleFromCRD(DefaultContext, &v1.Role{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: uid()},
			Spec: v1.RoleSpec{Rules: []v1.RoleRule{
				{Name: name, Action: action, Resource: &v1.ScopeReference{ScopeRef: scope}},
			}},
		})).To(Succeed())
		Expect(db.PersistRoleBindingFromCRD(DefaultContext, &v1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: uid()},
			Spec: v1.RoleBindingSpec{Role: name, Subjects: v1.RoleBindingSubjects{
				Plugins: []v1.PermissionGroupSelector{{Namespace: namespace, Name: entry.Name}},
			}},
		})).To(Succeed())
	}

	lookups := map[string]func() *pluginAPI.GetConnectionRequest{
		"by type": func() *pluginAPI.GetConnectionRequest {
			return &pluginAPI.GetConnectionRequest{Lookup: &pluginAPI.GetConnectionRequest_Type{Type: "postgres"}}
		},
		"by label": func() *pluginAPI.GetConnectionRequest {
			return &pluginAPI.GetConnectionRequest{Lookup: &pluginAPI.GetConnectionRequest_Label{Label: "primary"}}
		},
		"by id": func() *pluginAPI.GetConnectionRequest {
			return &pluginAPI.GetConnectionRequest{Lookup: &pluginAPI.GetConnectionRequest_ConnectionId{ConnectionId: conn.ID.String()}}
		},
		"for a config": func() *pluginAPI.GetConnectionRequest {
			return &pluginAPI.GetConnectionRequest{Lookup: &pluginAPI.GetConnectionRequest_ConfigItemId{ConfigItemId: config.ID.String()}}
		},
	}

	lookup := func(entry *plugin.Entry, req *pluginAPI.GetConnectionRequest) (*pluginAPI.ResolvedConnection, error) {
		ctx := gocontext.WithValue(gocontext.Background(), invocationClaimsContextKey{}, &plugin.InvocationTokenClaims{Plugin: entry.ID})
		return svc.GetConnection(ctx, req)
	}

	ginkgo.BeforeAll(func() {
		Expect(icrbac.Init(DefaultContext)).To(Succeed())
		svc = NewGRPCService(DefaultContext)

		conn = models.Connection{
			ID:        uuid.New(),
			Name:      "plugin-connection",
			Namespace: namespace,
			Source:    models.SourceUI,
			Type:      models.ConnectionTypePostgres,
			URL:       "postgres://$(username):$(password)@db.example.com/app",
			Username:  "app",
			Password:  password,
		}
		Expect(DefaultContext.DB().Create(&conn).Error).To(Succeed())

		scraper = models.ConfigScraper{ID: uuid.New(), Name: "plugin-connection-sql", Namespace: namespace, Source: models.SourceUI,
			Spec: fmt.Sprintf(`{"sql":[{"connection":%q}]}`, connectionRef)}
		inlineScraper = models.ConfigScraper{ID: uuid.New(), Name: "plugin-connection-inline", Namespace: namespace, Source: models.SourceUI,
			Spec: `{"kubernetes":[{"kubeconfig":{"value":"apiVersion: v1"}}]}`}
		Expect(DefaultContext.DB().Create(&scraper).Error).To(Succeed())
		Expect(DefaultContext.DB().Create(&inlineScraper).Error).To(Succeed())

		config = models.ConfigItem{ID: uuid.New(), Name: lo.ToPtr("db"), Type: lo.ToPtr("Postgres::Database"), ConfigClass: "Database", ScraperID: lo.ToPtr(scraper.ID.String())}
		inlineConfig = models.ConfigItem{ID: uuid.New(), Name: lo.ToPtr("pod"), Type: lo.ToPtr("Kubernetes::Pod"), ConfigClass: "Pod", ScraperID: lo.ToPtr(inlineScraper.ID.String())}
		Expect(DefaultContext.DB().Create(&config).Error).To(Succeed())
		Expect(DefaultContext.DB().Create(&inlineConfig).Error).To(Succeed())

		unbound, reader, user = register("unbound"), register("reader"), register("user")

		// Checks read stored membership, so the connection must exist before the Scopes are built.
		// Reads of connections can only select them all; connection:use selects the one connection.
		for name, selector := range map[string]v1.ScopeResourceRef{
			"all-connections":   {Name: "*"},
			"plugin-connection": {Name: conn.Name, Namespace: namespace},
		} {
			Expect(db.PersistScopeFromCRD(DefaultContext, &v1.Scope{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: uid()},
				Spec:       v1.ScopeSpec{Targets: []v1.ScopeTarget{{Connection: &v1.ScopeConnectionSelector{ScopeResourceRef: selector}}}},
			})).To(Succeed())
		}
		bind("read-connections", policy.ActionRead, "all-connections", reader)
		bind("use-connection", adapter.ActionConnectionUse, "plugin-connection", user)
		Expect(dutyRBAC.ReloadPolicy()).To(Succeed())
	})

	ginkgo.AfterAll(func() {
		for _, entry := range []*plugin.Entry{unbound, reader, user} {
			if entry != nil {
				plugin.DefaultRegistry.Remove(entry.ID)
			}
		}
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Plugin{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.RoleBinding{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Role{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Scope{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Delete(&[]models.ConfigItem{config, inlineConfig}).Error).To(Succeed())
		Expect(DefaultContext.DB().Delete(&[]models.ConfigScraper{scraper, inlineScraper}).Error).To(Succeed())
		Expect(DefaultContext.DB().Delete(&conn).Error).To(Succeed())
		Expect(dutyRBAC.ReloadPolicy()).To(Succeed())
	})

	// The plugin bound to connection:use looks the connection up first, so the others are checked with it cached.
	for name, req := range lookups {
		ginkgo.It("gives credentials to a plugin bound to connection:use, "+name, func() {
			resolved, err := lookup(user, req())
			Expect(err).ToNot(HaveOccurred())
			Expect(resolved.Username).To(Equal("app"))
			Expect(resolved.Password).To(Equal(password))
		})
	}

	for name, req := range lookups {
		ginkgo.It("refuses a plugin without a binding, "+name, func() {
			_, err := lookup(unbound, req())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("access denied"))
		})
	}

	for name, req := range lookups {
		ginkgo.It("shows a plugin bound to read alone the connection without its credentials, "+name, func() {
			resolved, err := lookup(reader, req())
			Expect(err).ToNot(HaveOccurred())
			Expect(resolved.Type).To(Equal(models.ConnectionTypePostgres))
			Expect(resolved.Url).To(Equal(conn.URL))
			Expect(resolved.Username).To(Equal(db.MaskedValue))
			Expect(resolved.Password).To(Equal(db.MaskedValue))
		})
	}

	ginkgo.It("refuses every plugin credentials a scraper holds inline, as nothing can grant them", func() {
		for _, entry := range []*plugin.Entry{unbound, reader, user} {
			_, err := lookup(entry, &pluginAPI.GetConnectionRequest{Lookup: &pluginAPI.GetConnectionRequest_ConfigItemId{ConfigItemId: inlineConfig.ID.String()}})
			Expect(err).To(HaveOccurred(), entry.Name)
			Expect(err.Error()).To(ContainSubstring("can't be granted to a plugin"))
		}
	})
})
