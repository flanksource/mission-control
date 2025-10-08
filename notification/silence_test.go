package notification_test

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/types"
	"github.com/flanksource/incident-commander/notification"
	"github.com/google/uuid"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
)

func mapToBase64Str(m map[string]any) string {
	b, _ := json.Marshal(m)
	return base64.StdEncoding.EncodeToString(b)
}

func TestSilenceSaveRequest_Validate(t *testing.T) {
	type fields struct {
		NotificationSilenceResource models.NotificationSilenceResource
		From                        string
		Until                       string
		Description                 string
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			name: "empty from",
			fields: fields{
				NotificationSilenceResource: models.NotificationSilenceResource{},
				From:                        "",
				Until:                       "now+2d",
			},
			wantErr: true,
		},
		{
			name: "empty until",
			fields: fields{
				NotificationSilenceResource: models.NotificationSilenceResource{},
				From:                        "now",
				Until:                       "",
			},
			wantErr: true,
		},
		{
			name: "empty resource",
			fields: fields{
				NotificationSilenceResource: models.NotificationSilenceResource{},
				From:                        "now",
				Until:                       "now+2d",
			},
			wantErr: true,
		},
		{
			name: "valid",
			fields: fields{
				NotificationSilenceResource: models.NotificationSilenceResource{
					ConfigID: lo.ToPtr(uuid.NewString()),
				},
				From:  "now",
				Until: "now+2d",
			},
		},
		{
			name: "complete but invalid",
			fields: fields{
				NotificationSilenceResource: models.NotificationSilenceResource{
					ConfigID: lo.ToPtr(uuid.NewString()),
				},
				From:  "now",
				Until: "now-1m",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &notification.SilenceSaveRequest{
				NotificationSilenceResource: tt.fields.NotificationSilenceResource,
				From:                        &tt.fields.From,
				Until:                       &tt.fields.Until,
				Description:                 &tt.fields.Description,
			}
			if err := tr.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("SilenceSaveRequest.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func createTestNotificationSendHistories() []models.NotificationSendHistory {
	config1ID := dummy.LogisticsAPIDeployment.ID
	config2ID := dummy.LogisticsAPIReplicaSet.ID
	config3ID := dummy.LogisticsUIDeployment.ID
	check1ID := uuid.New()
	component1ID := uuid.New()

	getProps := func(id uuid.UUID) string {
		return mapToBase64Str(map[string]any{
			"id": id.String(),
		})
	}

	return []models.NotificationSendHistory{
		{
			ID:             uuid.New(),
			ResourceID:     config1ID,
			SourceEvent:    "config.unhealthy",
			NotificationID: dummy.NoMatchNotification.ID,
			Status:         models.NotificationStatusSent,
			Payload: types.JSONStringMap{
				"properties":  getProps(config1ID),
				"resource_id": config1ID.String(),
			},
			CreatedAt: time.Now().Add(-time.Hour),
		},
		{
			ID:             uuid.New(),
			ResourceID:     config2ID,
			SourceEvent:    "config.updated",
			NotificationID: dummy.NoMatchNotification.ID,
			Status:         models.NotificationStatusSent,
			Payload: types.JSONStringMap{
				"properties": getProps(config2ID),
				"id":         config2ID.String(),
			},
			CreatedAt: time.Now().Add(-30 * time.Minute),
		},
		{
			ID:             uuid.New(),
			ResourceID:     config3ID,
			SourceEvent:    "config.healthy",
			NotificationID: dummy.NoMatchNotification.ID,
			Status:         models.NotificationStatusSent,
			Payload: types.JSONStringMap{
				"properties": getProps(config3ID),
				"id":         config3ID.String(),
			},
			CreatedAt: time.Now().Add(-15 * time.Minute),
		},
		{
			ID:             uuid.New(),
			ResourceID:     check1ID,
			SourceEvent:    "check.failed",
			NotificationID: dummy.NoMatchNotification.ID,
			Status:         models.NotificationStatusSent,
			Payload: types.JSONStringMap{
				"properties": getProps(check1ID),
				"id":         check1ID.String(),
			},
			CreatedAt: time.Now().Add(-10 * time.Minute),
		},
		{
			ID:             uuid.New(),
			ResourceID:     component1ID,
			SourceEvent:    "component.unhealthy",
			NotificationID: dummy.NoMatchNotification.ID,
			Status:         models.NotificationStatusSent,
			Payload: types.JSONStringMap{
				"properties": getProps(component1ID),
				"id":         component1ID.String(),
			},
			CreatedAt: time.Now().Add(-5 * time.Minute),
		},
		{
			ID:             uuid.New(),
			ResourceID:     dummy.KubernetesNodeA.ID,
			SourceEvent:    "config.unhealthy",
			NotificationID: dummy.NoMatchNotification.ID,
			Status:         models.NotificationStatusSent,
			Payload: types.JSONStringMap{
				"properties": getProps(dummy.KubernetesNodeA.ID),
				"id":         dummy.KubernetesNodeA.ID.String(),
			},
			CreatedAt: time.Now().Add(-5 * time.Minute),
		},
	}
}

var _ = ginkgo.Describe("Notification silence preview", func() {
	ginkgo.It("should check silence via resource_id", func() {
		testHistories := createTestNotificationSendHistories()

		err := DefaultContext.DB().Exec(`UPDATE config_items SET path=? WHERE id = ?`, dummy.EKSCluster.ID.String(), dummy.KubernetesNodeA.ID.String()).Error
		Expect(err).To(BeNil())

		tests := []struct {
			name             string
			histories        []models.NotificationSendHistory
			resourceID       string
			resourceType     string
			recursive        bool
			expectedSilenced int
			expectedIDs      []uuid.UUID
		}{
			{
				name:             "single matching resource ID",
				histories:        testHistories,
				resourceID:       testHistories[0].ResourceID.String(),
				resourceType:     "config",
				expectedSilenced: 1,
				expectedIDs:      []uuid.UUID{testHistories[0].ID},
			},
			{
				name:             "matching recursive resource_id ID",
				histories:        testHistories,
				resourceID:       dummy.EKSCluster.ID.String(),
				resourceType:     "config",
				expectedSilenced: 1,
				recursive:        true,
				expectedIDs:      []uuid.UUID{testHistories[5].ID},
			},
			{
				name:             "no matching resource ID",
				histories:        testHistories,
				resourceID:       uuid.New().String(),
				resourceType:     "config",
				expectedSilenced: 0,
				expectedIDs:      []uuid.UUID{},
			},
			{
				name:             "empty resource ID",
				histories:        testHistories,
				resourceID:       "",
				resourceType:     "",
				expectedSilenced: 0,
				expectedIDs:      []uuid.UUID{},
			},
			{
				name:             "empty histories",
				histories:        []models.NotificationSendHistory{},
				resourceID:       testHistories[0].ResourceID.String(),
				expectedSilenced: 0,
				resourceType:     "config",
				expectedIDs:      []uuid.UUID{},
			},
		}

		for _, tt := range tests {
			silenced, err := notification.CanSilence(DefaultContext, tt.histories, notification.CanSilenceParams{
				ResourceID:   tt.resourceID,
				Recursive:    tt.recursive,
				ResourceType: tt.resourceType,
			})
			Expect(err).To(BeNil())

			Expect(tt.expectedSilenced).To(Equal(len(silenced)))

			if len(tt.expectedIDs) > 0 {
				actualIDs := make([]uuid.UUID, len(silenced))
				for i, h := range silenced {
					actualIDs[i] = h.ID
				}
				Expect(tt.expectedIDs).To(HaveExactElements(actualIDs))
			}

		}
	})

	ginkgo.It("should check silence via filter", func() {
		testConfig1 := &models.ConfigItem{
			ID:          uuid.New(),
			Name:        lo.ToPtr("test-config-1"),
			Type:        lo.ToPtr("Kubernetes::Deployment"),
			ConfigClass: "Deployment",
			Labels: &types.JSONStringMap{
				"app": "user-api",
			},
		}

		testConfig2 := &models.ConfigItem{
			ID:          uuid.New(),
			Name:        lo.ToPtr("test-config-2"),
			Type:        lo.ToPtr("Kubernetes::Deployment"),
			ConfigClass: "Deployment",
			Labels: &types.JSONStringMap{
				"app": "payment-api",
			},
		}

		ctx := DefaultContext
		ctx.DB().Create([]*models.ConfigItem{testConfig1, testConfig2})

		// Create test data with base64 encoded properties
		props1 := map[string]any{
			"id": testConfig1.ID.String(),
		}
		props2 := map[string]any{
			"id": testConfig2.ID.String(),
		}
		testHistories := []models.NotificationSendHistory{
			{
				ID:             uuid.New(),
				ResourceID:     testConfig1.ID,
				SourceEvent:    "config.unhealthy",
				NotificationID: uuid.New(),
				Status:         models.NotificationStatusSent,
				Payload: types.JSONStringMap{
					"properties": mapToBase64Str(props1),
				},
				CreatedAt: time.Now().Add(-time.Hour),
			},
			{
				ID:             uuid.New(),
				ResourceID:     testConfig2.ID,
				SourceEvent:    "config.updated",
				NotificationID: uuid.New(),
				Status:         models.NotificationStatusSent,
				Payload: types.JSONStringMap{
					"properties": mapToBase64Str(props2),
				},
				CreatedAt: time.Now().Add(-30 * time.Minute),
			},
		}

		tests := []struct {
			name             string
			histories        []models.NotificationSendHistory
			filter           string
			expectedSilenced int
			description      string
			willErr          bool
		}{
			{
				name:             "filter by high severity",
				histories:        testHistories,
				filter:           "config.labels.app == 'user-api'",
				expectedSilenced: 1,
				description:      "Should match notifications with high severity",
			},
			{
				name:             "filter by service name",
				histories:        testHistories,
				filter:           "config.labels.app == 'payment-api'",
				expectedSilenced: 1,
				description:      "Should match notifications from user-api service",
			},
			{
				name:             "no matching filter",
				histories:        testHistories,
				filter:           "properties.severity == 'critical'",
				expectedSilenced: 0,
				description:      "Should not match any notifications",
				willErr:          true,
			},
			{
				name:             "should match both",
				histories:        testHistories,
				filter:           "config.type == 'Kubernetes::Deployment'",
				expectedSilenced: 2,
				description:      "should match both",
			},
			{
				name:             "empty histories",
				histories:        []models.NotificationSendHistory{},
				filter:           "properties.severity == 'high'",
				expectedSilenced: 0,
				description:      "Should handle empty histories",
			},
		}

		for _, tt := range tests {
			silenced, err := notification.CanSilenceViaFilter(ctx, tt.histories, tt.filter)
			if !tt.willErr {
				Expect(err).To(BeNil())
				Expect(silenced).To(HaveLen(tt.expectedSilenced))
			} else {
				Expect(err).ToNot(BeNil())
			}
		}
	})

	ginkgo.It("should silence existing data with filter", func() {
		nonStringProps := map[string]any{
			"id": dummy.KubernetesNodeB.ID,
		}

		nonStringPropsHistory := models.NotificationSendHistory{
			ID:             uuid.New(),
			ResourceID:     dummy.KubernetesNodeB.ID,
			SourceEvent:    "config.unhealthy",
			NotificationID: uuid.New(),
			Status:         models.NotificationStatusSent,
			Payload: types.JSONStringMap{
				"properties": mapToBase64Str(nonStringProps),
			},
			CreatedAt: time.Now(),
		}

		tests := []struct {
			name      string
			histories []models.NotificationSendHistory
			filter    string
			expected  []uuid.UUID
		}{
			{
				name:      "non-string properties should work as fallback",
				histories: []models.NotificationSendHistory{nonStringPropsHistory},
				filter:    "config.config_class == 'Node'",
				expected:  uuid.UUIDs{nonStringPropsHistory.ID},
			},
		}

		for _, tt := range tests {
			silenced, err := notification.CanSilenceViaFilter(DefaultContext, tt.histories, tt.filter)
			silencedIDs := lo.Map(silenced, func(n models.NotificationSendHistory, _ int) uuid.UUID { return n.ID })
			Expect(err).To(BeNil())
			Expect(silencedIDs).To(Equal(tt.expected))
		}
	})

	ginkgo.It("should silence via selectors", func() {
		ctx := DefaultContext

		history1 := uuid.New()
		history2 := uuid.New()

		testHistories := []models.NotificationSendHistory{
			{
				ID:             history1,
				ResourceID:     dummy.KubernetesNodeB.ID,
				SourceEvent:    "config.unhealthy",
				NotificationID: uuid.New(),
				Status:         models.NotificationStatusSent,
				Payload:        types.JSONStringMap{},
				CreatedAt:      time.Now(),
			},
			{
				ID:             history2,
				ResourceID:     dummy.LogisticsWorker.ID,
				SourceEvent:    "component.unhealthy",
				NotificationID: uuid.New(),
				Status:         models.NotificationStatusSent,
				Payload:        types.JSONStringMap{},
				CreatedAt:      time.Now(),
			},
		}

		tests := []struct {
			name             string
			histories        []models.NotificationSendHistory
			selectors        types.ResourceSelectors
			expectedSilenced []uuid.UUID
			description      string
		}{
			{
				name:      "empty selectors",
				histories: testHistories,
				selectors: types.ResourceSelectors{{
					Types: []string{"Kubernetes::Node"},
					Name:  "node-b",
				}},
				expectedSilenced: []uuid.UUID{history1},
				description:      "Should handle empty selectors",
			},
			{
				name:      "empty histories",
				histories: []models.NotificationSendHistory{},
				selectors: types.ResourceSelectors{
					{
						LabelSelector: "app=test-app",
					},
				},
				expectedSilenced: uuid.UUIDs{},
				description:      "Should handle empty histories",
			},
		}

		for _, tt := range tests {
			silenced, err := notification.CanSilenceViaSelectors(ctx, tt.histories, tt.selectors)
			silencedIDs := lo.Map(silenced, func(n models.NotificationSendHistory, _ int) uuid.UUID { return n.ID })
			Expect(err).To(BeNil())
			Expect(silencedIDs).To(Equal(tt.expectedSilenced))
		}
	})
})
