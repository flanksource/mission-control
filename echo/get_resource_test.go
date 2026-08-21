package echo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/google/uuid"
	echov4 "github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("GET /resources/:id", func() {
	get := func(id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/resources/"+id, nil).WithContext(DefaultContext)
		recorder := httptest.NewRecorder()
		c := echov4.New().NewContext(req, recorder)
		c.SetParamNames("id")
		c.SetParamValues(id)

		// WriteError writes the response and returns nil, so a handler error here would mean the
		// status never reached the recorder at all.
		Expect(GetResource(c)).To(Succeed())
		return recorder
	}

	// A miss used to answer 500 with gorm's bare "record not found", which reads as a server fault
	// for what is the most ordinary client mistake there is.
	ginkgo.It("answers an id no config item has with 404 naming that id", func() {
		// Freshly generated so the process-wide config cache can never hold a hit for it.
		id := uuid.New().String()

		recorder := get(id)

		Expect(recorder.Code).To(Equal(http.StatusNotFound))
		// The id has to survive into the body: it is what makes the CLI error actionable, and a
		// generic "not found" would satisfy the status assertion while losing it.
		Expect(recorder.Body.String()).To(ContainSubstring(id))
	})

	ginkgo.It("answers an id that is not a uuid with 400 rather than a driver error", func() {
		recorder := get("not-a-uuid")

		Expect(recorder.Code).To(Equal(http.StatusBadRequest))
		Expect(recorder.Body.String()).To(ContainSubstring("not-a-uuid"))
	})

	ginkgo.It("returns the config item an id does name", func() {
		recorder := get(dummy.EKSCluster.ID.String())

		Expect(recorder.Code).To(Equal(http.StatusOK))
		var item models.ConfigItem
		Expect(json.Unmarshal(recorder.Body.Bytes(), &item)).To(Succeed())
		// The id, not the name: an empty body would decode cleanly and still pass a name assertion.
		Expect(item.ID).To(Equal(dummy.EKSCluster.ID))
	})
})
