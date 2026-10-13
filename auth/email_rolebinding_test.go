package auth

import (
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	icrbac "github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/rbactest"
	"github.com/flanksource/incident-commander/vars"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

type clerkEmailTransport func(*http.Request) (*http.Response, error)

func (f clerkEmailTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

var _ = ginkgo.Describe("Verified email RoleBindings", func() {
	var person models.Person
	var namespace, otherEmail string

	ginkgo.BeforeEach(func() {
		oldMode := vars.AuthMode
		ginkgo.DeferCleanup(func() { vars.AuthMode = oldMode })
		vars.AuthMode = Kratos
		person = models.Person{ID: uuid.New(), Name: "Email binding subject", Email: uuid.NewString() + "@example.com"}
		otherEmail = uuid.NewString() + "@example.com"
		namespace = "verified-email-" + uuid.NewString()
		Expect(DefaultContext.DB().Create(&person).Error).To(Succeed())
		Expect(DefaultContext.DB().Exec(`CREATE TABLE IF NOT EXISTS identity_verifiable_addresses (
			id uuid PRIMARY KEY, identity_id uuid NOT NULL, value text NOT NULL, via text NOT NULL, verified boolean NOT NULL
		)`).Error).To(Succeed())
		ginkgo.DeferCleanup(func() {
			Expect(DefaultContext.DB().Exec("DELETE FROM identity_verifiable_addresses WHERE identity_id = ?", person.ID).Error).To(Succeed())
			Expect(DefaultContext.DB().Delete(&person).Error).To(Succeed())
			Expect(rbactest.DeleteNamespace(DefaultContext, namespace)).To(Succeed())
			FlushTokenCache()
		})
		Expect(DefaultContext.DB().Exec("INSERT INTO identity_verifiable_addresses (id, identity_id, value, via, verified) VALUES (?, ?, ?, 'email', false)", uuid.New(), person.ID, person.Email).Error).To(Succeed())
		Expect(rbactest.BindWholeType(DefaultContext, namespace, "email-reader", []string{person.Email, otherEmail}, []string{"config"}, []string{"read"})).To(Succeed())
	})

	canRead := func() bool {
		ctx := DefaultContext.WithUser(&person)
		allowed := dutyRBAC.HasPermission(ctx, person.ID.String(), &models.ABACAttribute{Config: models.ConfigItem{ID: person.ID}}, policy.ActionRead)
		Expect(icrbac.CheckContext(ctx, policy.ObjectCatalog, policy.ActionRead)).To(Equal(allowed))
		return allowed
	}
	reload := func() {
		Expect(RebuildOIDCBindings(DefaultContext)).To(Succeed())
	}
	verify := func() {
		Expect(DefaultContext.DB().Exec("UPDATE identity_verifiable_addresses SET verified = true WHERE identity_id = ? AND value = ?", person.ID, person.Email).Error).To(Succeed())
		reload()
	}

	ginkgo.It("grants nothing for a named unverified email", func() {
		Expect(canRead()).To(BeFalse())
	})

	ginkgo.It("grants the binding after verification", func() {
		verify()
		Expect(canRead()).To(BeTrue())
	})

	ginkgo.It("removes the old assignment when the email changes to an unverified address", func() {
		verify()
		Expect(canRead()).To(BeTrue())
		Expect(DefaultContext.DB().Exec("UPDATE identity_verifiable_addresses SET value = ?, verified = false WHERE identity_id = ?", otherEmail, person.ID).Error).To(Succeed())
		Expect(DefaultContext.DB().Model(&person).Update("email", otherEmail).Error).To(Succeed())
		reload()
		Expect(canRead()).To(BeFalse())
		roles, err := dutyRBAC.Enforcer().GetRolesForUser(person.ID.String())
		Expect(err).ToNot(HaveOccurred())
		Expect(roles).ToNot(ContainElement(models.BindingPrincipal(namespace, "email-reader")))
	})

	ginkgo.It("fails closed when an unrelated policy error interrupts revocation", func() {
		verify()
		Expect(canRead()).To(BeTrue())
		group := models.PermissionGroup{ID: uuid.New(), Name: "broken-" + uuid.NewString(), Selectors: []byte(`[]`)}
		Expect(DefaultContext.DB().Create(&group).Error).To(Succeed())
		ginkgo.DeferCleanup(func() {
			Expect(DefaultContext.DB().Delete(&group).Error).To(Succeed())
		})
		Expect(DefaultContext.DB().Exec("UPDATE identity_verifiable_addresses SET verified = false WHERE identity_id = ?", person.ID).Error).To(Succeed())
		Expect(icrbac.ReloadPolicy()).ToNot(Succeed())
		Expect(canRead()).To(BeFalse())
	})

	ginkgo.It("does not let an in-flight RLS calculation cache revoked email grants", func() {
		verify()
		FlushTokenCache()
		type pausedRequest struct{}
		paused, resume := make(chan struct{}), make(chan struct{})
		var pauseOnce, resumeOnce sync.Once
		callback := "verified-email-" + person.ID.String()
		Expect(DefaultContext.DB().Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
			if tx.Statement.Table == "role_bindings" && tx.Statement.Context.Value(pausedRequest{}) == true {
				pauseOnce.Do(func() { close(paused); <-resume })
			}
		})).To(Succeed())
		ginkgo.DeferCleanup(func() {
			resumeOnce.Do(func() { close(resume) })
			Expect(DefaultContext.DB().Callback().Query().Remove(callback)).To(Succeed())
		})
		done := make(chan error, 1)
		go func() {
			defer ginkgo.GinkgoRecover()
			_, err := GetRLSPayload(DefaultContext.WithUser(&person).WithValue(pausedRequest{}, true))
			done <- err
		}()
		Eventually(paused).Should(BeClosed())
		Expect(DefaultContext.DB().Exec("UPDATE identity_verifiable_addresses SET verified = false WHERE identity_id = ?", person.ID).Error).To(Succeed())
		Expect(icrbac.ReloadPolicy()).To(Succeed())
		resumeOnce.Do(func() { close(resume) })
		Eventually(done).Should(Receive(BeNil()))
		payload, err := GetRLSPayload(DefaultContext.WithUser(&person))
		Expect(err).ToNot(HaveOccurred())
		Expect(payload.Config).To(BeNil())
	})

	ginkgo.It("matches only the verified one of two Kratos addresses", func() {
		Expect(DefaultContext.DB().Exec("INSERT INTO identity_verifiable_addresses (id, identity_id, value, via, verified) VALUES (?, ?, ?, 'email', true)", uuid.New(), person.ID, otherEmail).Error).To(Succeed())
		Expect(rbactest.BindWholeType(DefaultContext, namespace, "unverified-reader", []string{person.Email}, []string{"playbook"}, []string{"read"})).To(Succeed())
		Expect(canRead()).To(BeTrue())
		ctx := DefaultContext.WithUser(&person)
		Expect(dutyRBAC.HasPermission(ctx, person.ID.String(), &models.ABACAttribute{Playbook: models.Playbook{ID: uuid.New()}}, policy.ActionRead)).To(BeFalse())
	})

	ginkgo.It("does not treat basic-auth emails as verified", func() {
		verify()
		vars.AuthMode = Basic
		reload()
		Expect(canRead()).To(BeFalse())
	})

	ginkgo.It("reconciles Clerk verification and fails closed on unknown state or API failure", func() {
		vars.AuthMode = Clerk
		person.ExternalID = "user_" + uuid.NewString()
		Expect(DefaultContext.DB().Model(&person).Update("external_id", person.ExternalID).Error).To(Succeed())
		ginkgo.GinkgoT().Setenv("CLERK_SECRET_KEY", "test-only-key")
		oldTransport := http.DefaultTransport
		ginkgo.DeferCleanup(func() { http.DefaultTransport = oldTransport })
		status, email, httpStatus := "unverified", person.Email, http.StatusOK
		http.DefaultTransport = clerkEmailTransport(func(r *http.Request) (*http.Response, error) {
			Expect(r.URL.Host).To(Equal("api.clerk.com"))
			body := fmt.Sprintf(`[{"id":%q,"email_addresses":[{"email_address":%q,"verification":{"status":%q}}]}]`, person.ExternalID, email, status)
			if !slices.Contains(r.URL.Query()["email_address"], email) {
				body = "[]"
			}
			return &http.Response{StatusCode: httpStatus, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
		})
		reload()
		Expect(canRead()).To(BeFalse())
		status = "verified"
		reload()
		Expect(canRead()).To(BeTrue())
		email, status = otherEmail, "unverified"
		reload()
		Expect(canRead()).To(BeFalse())
		status = ""
		reload()
		Expect(canRead()).To(BeFalse())
		email, status = person.Email, "verified"
		reload()
		Expect(canRead()).To(BeTrue())
		httpStatus = http.StatusServiceUnavailable
		reload()
		Expect(canRead()).To(BeFalse())
	})
})
