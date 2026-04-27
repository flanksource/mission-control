package notification

import (
	"time"

	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("NotificationEventPayload original event", func() {
	ginkgo.It("restores serialized event properties", func() {
		eventID := uuid.MustParse("9b7f2a8e-1111-4444-8888-4e28c2f6d901")
		resourceID := uuid.MustParse("11111111-2222-3333-4444-555555555555")
		createdAt := time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC)

		payload := NotificationEventPayload{
			EventID:        eventID,
			EventName:      "config.unhealthy",
			ResourceID:     resourceID,
			EventCreatedAt: createdAt,
			Properties:     []byte(`{"status":"CrashLoopBackOff","description":"deployment/api in namespace prod has 3 crashing pods","namespace":"prod","name":"api"}`),
		}

		event, err := payload.originalEvent()

		Expect(err).ToNot(HaveOccurred())
		Expect(event.Name).To(Equal("config.unhealthy"))
		Expect(event.EventID).To(Equal(eventID))
		Expect(event.CreatedAt).To(Equal(createdAt))
		Expect(event.Properties).To(HaveKeyWithValue("status", "CrashLoopBackOff"))
		Expect(event.Properties).To(HaveKeyWithValue("description", "deployment/api in namespace prod has 3 crashing pods"))
		Expect(event.Properties).To(HaveKeyWithValue("namespace", "prod"))
		Expect(event.Properties).To(HaveKeyWithValue("name", "api"))
	})

	ginkgo.It("returns an error for invalid serialized properties", func() {
		payload := NotificationEventPayload{
			EventID:    uuid.MustParse("9b7f2a8e-1111-4444-8888-4e28c2f6d901"),
			EventName:  "config.unhealthy",
			Properties: []byte(`{"status":`),
		}

		_, err := payload.originalEvent()

		Expect(err).To(MatchError(ContainSubstring("failed to unmarshal properties")))
	})
})
