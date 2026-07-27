package echo

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	echov4 "github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("playbook PostgREST writes", func() {
	var createdIDs []uuid.UUID

	ginkgo.AfterEach(func() {
		if len(createdIDs) > 0 {
			Expect(DefaultContext.DB().Unscoped().Delete(&models.Playbook{}, "id IN ?", createdIDs).Error).ToNot(HaveOccurred())
		}
		createdIDs = nil
	})

	newPlaybook := func(source string) models.Playbook {
		playbook := models.Playbook{
			ID:        uuid.New(),
			Namespace: "default",
			Name:      "postgrest-" + uuid.NewString(),
			Spec:      types.JSON(`{"actions":[{"name":"echo","exec":{"script":"echo ok"}}]}`),
			Source:    source,
		}
		Expect(DefaultContext.DB().Create(&playbook).Error).ToNot(HaveOccurred())
		createdIDs = append(createdIDs, playbook.ID)
		return playbook
	}

	execute := func(method, target string, body any) (*httptest.ResponseRecorder, bool) {
		var payload []byte
		if body != nil {
			var err error
			payload, err = json.Marshal(body)
			Expect(err).ToNot(HaveOccurred())
		}
		req := httptest.NewRequest(method, target, bytes.NewReader(payload)).WithContext(DefaultContext)
		req.Header.Set(echov4.HeaderContentType, echov4.MIMEApplicationJSON)
		recorder := httptest.NewRecorder()
		ctx := echov4.New().NewContext(req, recorder)
		called := false
		handler := postgrestInterceptor(func(c echov4.Context) error {
			called = true
			return c.NoContent(http.StatusNoContent)
		})
		err := handler(ctx)
		if err != nil {
			ctx.Echo().HTTPErrorHandler(err, ctx)
		}
		return recorder, called
	}

	ginkgo.It("applies generated schema validation before proxying writes", func() {
		response, called := execute(http.MethodPost, "/db/playbooks", map[string]any{
			"namespace": "default",
			"name":      "invalid",
			"source":    models.SourceUI,
			"spec": map[string]any{
				"actions":    []any{map[string]any{"name": "echo", "exec": map[string]any{"script": "echo ok"}}},
				"unexpected": true,
			},
		})

		Expect(called).To(BeFalse())
		Expect(response.Code).To(Equal(http.StatusBadRequest))
		Expect(response.Body.String()).To(ContainSubstring("Additional property unexpected is not allowed"))
	})

	ginkgo.It("allows valid UI playbook updates", func() {
		playbook := newPlaybook(models.SourceUI)
		response, called := execute(http.MethodPatch, "/db/playbooks?id=eq."+playbook.ID.String(), map[string]any{
			"source": models.SourceUI,
			"spec": map[string]any{
				"actions": []any{map[string]any{"name": "echo", "exec": map[string]any{"script": "echo changed"}}},
			},
		})

		Expect(called).To(BeTrue())
		Expect(response.Code).To(Equal(http.StatusNoContent))
	})

	for _, source := range []string{models.SourceCRD, models.SourceConfigFile} {
		ginkgo.It("rejects updates to "+source+" playbooks", func() {
			playbook := newPlaybook(source)
			response, called := execute(http.MethodPatch, "/db/playbooks?id=eq."+playbook.ID.String(), map[string]any{
				"spec": map[string]any{
					"actions": []any{map[string]any{"name": "echo", "exec": map[string]any{"script": "echo changed"}}},
				},
			})

			Expect(called).To(BeFalse())
			Expect(response.Code).To(Equal(http.StatusConflict))
			Expect(response.Body.String()).To(ContainSubstring("not created through the API"))
		})
	}

	ginkgo.It("allows deletes of API-created playbooks", func() {
		playbook := newPlaybook(models.SourceUI)
		response, called := execute(http.MethodDelete, "/db/playbooks?id=eq."+playbook.ID.String(), nil)

		Expect(called).To(BeTrue())
		Expect(response.Code).To(Equal(http.StatusNoContent))
	})

	ginkgo.It("rejects deletes of externally managed playbooks", func() {
		playbook := newPlaybook(models.SourceConfigFile)
		response, called := execute(http.MethodDelete, "/db/playbooks?id=eq."+playbook.ID.String(), nil)

		Expect(called).To(BeFalse())
		Expect(response.Code).To(Equal(http.StatusConflict))
		Expect(response.Body.String()).To(ContainSubstring("not created through the API"))
	})

	ginkgo.It("rejects externally managed sources on create", func() {
		response, called := execute(http.MethodPost, "/db/playbooks", map[string]any{
			"namespace": "default",
			"name":      "config-file",
			"source":    models.SourceConfigFile,
			"spec": map[string]any{
				"actions": []any{map[string]any{"name": "echo", "exec": map[string]any{"script": "echo ok"}}},
			},
		})

		Expect(called).To(BeFalse())
		Expect(response.Code).To(Equal(http.StatusBadRequest))
		Expect(response.Body.String()).To(ContainSubstring("playbook source must be"))
	})

	ginkgo.It("rejects source changes", func() {
		playbook := newPlaybook(models.SourceUI)
		response, called := execute(http.MethodPatch, "/db/playbooks?id=eq."+playbook.ID.String(), map[string]any{
			"source": models.SourceCRD,
		})

		Expect(called).To(BeFalse())
		Expect(response.Code).To(Equal(http.StatusBadRequest))
		Expect(response.Body.String()).To(ContainSubstring("source cannot be changed"))
	})

	ginkgo.It("requires an exact id for mutations", func() {
		response, called := execute(http.MethodPatch, "/db/playbooks?name=eq.any", map[string]any{
			"deleted_at": "now()",
		})

		Expect(called).To(BeFalse())
		Expect(response.Code).To(Equal(http.StatusBadRequest))
		Expect(response.Body.String()).To(ContainSubstring("exact id filter"))
	})
})
