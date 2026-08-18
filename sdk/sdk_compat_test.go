package sdk

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/flanksource/clicky/rpc"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/query"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	icapi "github.com/flanksource/incident-commander/api"
)

var (
	_ func(string, string, ...ClientOption) *Client = New
	_ func(string, string, ...ClientOption) *Client = NewWithAuthHeader
	_ func(TokenProvider) ClientOption              = WithTokenProvider
	_ func(string) ClientOption                     = WithUserAgent
	_ func(string) ClientOption                     = WithAccept

	_ func(*Client, context.Context) (*WhoamiResponse, int, error)                                                   = (*Client).Whoami
	_ func(*Client, string, string) (*models.Connection, error)                                                      = (*Client).GetConnection
	_ func(*Client, *models.Connection) error                                                                        = (*Client).SaveConnection
	_ func(*Client, context.Context) ([]rpc.RPCService, error)                                                       = (*Client).ListPluginRPCServices
	_ func(*Client, context.Context, string, string, []byte, string) ([]byte, int, error)                            = (*Client).DispatchPluginOperation
	_ func(*Client, string) (*TestResult, error)                                                                     = (*Client).TestConnection
	_ func(*Client, string, string, string, json.RawMessage) ([]byte, error)                                         = (*Client).InvokePluginOperation
	_ func(*Client, PlaybookListOptions) ([]icapi.PlaybookListItem, error)                                           = (*Client).ListPlaybooks
	_ func(*Client, PlaybookRunParams) (*PlaybookRunResponse, error)                                                 = (*Client).RunPlaybook
	_ func(*Client, string) (*PlaybookSummary, error)                                                                = (*Client).GetPlaybookRunStatus
	_ func(*Client, context.Context, query.SearchResourcesRequest) (*query.SearchResourcesResponse, error)           = (*Client).SearchCatalog
	_ func(*Client, context.Context, query.CatalogChangesSearchRequest) (*query.CatalogChangesSearchResponse, error) = (*Client).SearchCatalogChanges
	_ func(*Client, context.Context, string) (*models.ConfigItem, error)                                             = (*Client).GetCatalogItem
	_ func(*Client, context.Context, []string) ([]models.ConfigItem, error)                                          = (*Client).GetCatalogItems
	_ func(*Client, context.Context, string) (*CatalogChangeDetail, error)                                           = (*Client).GetCatalogChange
	_ func(*Client, context.Context, string) (*CatalogInsightDetail, error)                                          = (*Client).GetCatalogInsight
	_ func(*Client, context.Context, []string) ([]CatalogInsightDetail, error)                                       = (*Client).GetCatalogInsights
	_ func(*Client, context.Context, string) (*CatalogRelationships, error)                                          = (*Client).GetCatalogRelationships
	_ func(*Client, context.Context, PlaybookApplyParams) (*PlaybookApplyResult, error)                              = (*Client).ApplyPlaybook
	_ func(*Client, context.Context, uuid.UUID) (*models.Playbook, error)                                            = (*Client).DeletePlaybook
	_ func(*Client, context.Context, PlaybookRunListOptions) ([]models.PlaybookRun, error)                           = (*Client).ListPlaybookRuns
)

var _ = ginkgo.Describe("public SDK compatibility", func() {
	ginkgo.It("accesses the initialized delegated client concurrently", func() {
		client := New("http://mission-control.example", "token")
		delegates := make(chan any, 16)
		var group sync.WaitGroup
		for range 16 {
			group.Add(1)
			go func() {
				defer group.Done()
				delegates <- client.leanClient()
			}()
		}
		group.Wait()
		close(delegates)
		for delegate := range delegates {
			Expect(delegate).To(BeIdenticalTo(client.lean))
		}
	})

	ginkgo.It("retains the model-backed playbook fields", func() {
		params := PlaybookApplyParams{
			Namespace: "default", Name: "restart", Title: "Restart", Icon: "play",
			Description: "Restart a workload", Category: "Kubernetes", Spec: json.RawMessage(`{}`),
		}
		result := PlaybookApplyResult{Playbook: models.Playbook{ID: uuid.New()}}

		Expect(params.Title).To(Equal("Restart"))
		Expect(result.Playbook.ID).ToNot(Equal(uuid.Nil))
		Expect(PlaybookRunListOptions{Statuses: []models.PlaybookRunStatus{models.PlaybookRunStatusRunning}}.Statuses).To(HaveLen(1))
	})
})
