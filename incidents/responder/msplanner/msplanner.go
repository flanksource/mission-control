package msplanner

import (
	gocontext "context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	kiotaAbstractions "github.com/microsoft/kiota-abstractions-go"
	kiotaAuth "github.com/microsoft/kiota-authentication-azure-go"
	msgraphsdk "github.com/microsoftgraph/msgraph-sdk-go"
	"github.com/microsoftgraph/msgraph-sdk-go/groups"
	"github.com/microsoftgraph/msgraph-sdk-go/models"
	"github.com/microsoftgraph/msgraph-sdk-go/models/odataerrors"
	"github.com/microsoftgraph/msgraph-sdk-go/planner"

	"github.com/flanksource/duty/context"
	"github.com/flanksource/incident-commander/api"
)

const ResponderType = "ms_planner"

type MSPlannerTask struct {
	Title       string
	PlanID      string `mapstructure:"plan_id"`
	Priority    string
	Description string
	BucketID    string `mapstructure:"bucket_id"`
}

type PlanConfig struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	Buckets    []PlanBucket `json:"buckets"`
	Priorities []string     `json:"priorities"`
}

type PlanBucket struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type MSPlannerConfig struct {
	Plans map[string]PlanConfig `json:"plans"`
}

type MSPlannerClient struct {
	client  *msgraphsdk.GraphServiceClient
	groupID string
}

// Planner interprets values 0 and 1 as "urgent", 2, 3 and 4 as "important", 5, 6, and 7 as "medium", and 8, 9, and 10 as "low"
// By default, it sets the value 1 for "urgent", 3 for "important", 5 for "medium", and 9 for "low"
var taskPriorities = map[string]int32{
	"urgent":    1,
	"important": 3,
	"medium":    5,
	"low":       9,
}

func NewClient(ctx context.Context, team api.Team) (*MSPlannerClient, error) {

	teamSpec, err := team.GetSpec()
	if err != nil {
		return nil, err
	}
	client := teamSpec.ResponderClients.MSPlanner

	username, err := ctx.GetEnvValueFromCache(client.Username, api.Namespace)
	if err != nil {
		return nil, err
	}
	password, err := ctx.GetEnvValueFromCache(client.Password, api.Namespace)
	if err != nil {
		return nil, err
	}

	return newClient(
		client.TenantID,
		client.ClientID,
		client.GroupID,
		username,
		password,
	)
}

func newClient(tenantID, clientID, groupID, username, password string) (*MSPlannerClient, error) {
	cred, err := azidentity.NewUsernamePasswordCredential(tenantID, clientID, username, password, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating credentials: %w", err)
	}

	auth, err := kiotaAuth.NewAzureIdentityAuthenticationProvider(cred)
	if err != nil {
		return nil, fmt.Errorf("error authentication provider: %w", err)
	}

	adapter, err := msgraphsdk.NewGraphRequestAdapter(auth)
	if err != nil {
		return nil, fmt.Errorf("error creating adapter: %w", err)
	}
	client := msgraphsdk.NewGraphServiceClient(adapter)
	return &MSPlannerClient{client: client, groupID: groupID}, nil
}

func (c MSPlannerClient) CreateTask(opts MSPlannerTask) (models.PlannerTaskable, error) {
	body := models.NewPlannerTask()

	body.SetPlanId(&opts.PlanID)
	body.SetBucketId(&opts.BucketID)
	body.SetTitle(&opts.Title)

	if opts.Priority != "" {
		if priority, exists := taskPriorities[opts.Priority]; exists {
			body.SetPriority(&priority)
		}
	}

	if opts.Description != "" {
		descBody := models.NewPlannerTaskDetails()
		descBody.SetDescription(&opts.Description)
		body.SetDetails(descBody)
	}

	result, err := c.client.Planner().Tasks().Post(gocontext.Background(), body, nil)
	return result, openDataError(err)
}

func (c MSPlannerClient) AddComment(taskID, comment string) (string, error) {
	// MS Graph API does not return the ID of the comment created
	// so we just return a constant string for now
	commentID := "posted"

	task, err := c.client.Planner().Tasks().ByPlannerTaskId(taskID).Get(gocontext.Background(), nil)
	if err != nil {
		return commentID, openDataError(err)
	}

	post := models.NewPost()
	body := models.NewItemBody()
	contentType := models.TEXT_BODYTYPE
	body.SetContentType(&contentType)
	body.SetContent(&comment)
	post.SetBody(body)

	// If conversation thread exists, add a new reply
	if task.GetConversationThreadId() != nil {
		replyBody := groups.NewItemConversationsItemThreadsItemPostsItemReplyPostRequestBody()
		replyBody.SetPost(post)

		err = c.client.Groups().ByGroupId(c.groupID).Threads().ByConversationThreadId(*task.GetConversationThreadId()).Reply().Post(gocontext.Background(), replyBody, nil)
		return commentID, openDataError(err)
	}

	// Create a new conversation thread for the task
	convBody := models.NewConversationThread()
	topic := fmt.Sprintf("Conversation thread topic for taskID: %s", taskID)
	convBody.SetTopic(&topic)
	convBody.SetPosts([]models.Postable{post})

	result, err := c.client.Groups().ByGroupId(c.groupID).Threads().Post(gocontext.Background(), convBody, nil)
	if err != nil {
		return commentID, openDataError(err)
	}

	// Link the created conversation thread to the task
	etag := *task.GetAdditionalData()["@odata.etag"].(*string)
	headers := kiotaAbstractions.NewRequestHeaders()
	headers.Add("If-Match", etag)
	patchConfig := planner.TasksPlannerTaskItemRequestBuilderPatchRequestConfiguration{Headers: headers}

	requestBody := models.NewPlannerTask()
	requestBody.SetConversationThreadId(result.GetId())
	_, err = c.client.Planner().Tasks().ByPlannerTaskId(taskID).Patch(gocontext.Background(), requestBody, &patchConfig)
	return commentID, openDataError(err)
}

func (c MSPlannerClient) GetComments(taskID string) ([]api.Comment, error) {
	task, err := c.client.Planner().Tasks().ByPlannerTaskId(taskID).Get(gocontext.Background(), nil)
	if err != nil {
		return nil, openDataError(err)
	}

	var comments []api.Comment
	if task.GetConversationThreadId() == nil {
		return comments, nil
	}

	conversations, err := c.client.Groups().ByGroupId(c.groupID).Threads().ByConversationThreadId(*task.GetConversationThreadId()).Posts().Get(gocontext.Background(), nil)
	if err != nil {
		return nil, openDataError(err)
	}

	for _, conv := range conversations.GetValue() {
		comments = append(comments, api.Comment{
			Comment:           *conv.GetBody().GetContent(),
			ExternalCreatedBy: *conv.GetFrom().GetEmailAddress().GetName(),
			CreatedAt:         *conv.GetCreatedDateTime(),
		})
	}

	return comments, nil
}

func (c MSPlannerClient) GetConfig() (MSPlannerConfig, error) {
	var priorities []string
	for k := range taskPriorities {
		priorities = append(priorities, k)
	}

	config := make(map[string]PlanConfig)
	result, err := c.client.Groups().ByGroupId(c.groupID).Planner().Plans().Get(gocontext.Background(), nil)
	if err != nil {
		return MSPlannerConfig{}, openDataError(err)
	}

	for _, plan := range result.GetValue() {
		var buckets []PlanBucket
		planBuckets, err := c.client.Planner().Plans().ByPlannerPlanId(*plan.GetId()).Buckets().Get(gocontext.Background(), nil)
		if err != nil {
			return MSPlannerConfig{}, openDataError(err)
		}
		for _, bucket := range planBuckets.GetValue() {
			buckets = append(buckets, PlanBucket{
				ID:   *bucket.GetId(),
				Name: *bucket.GetName(),
			})
		}

		config[*plan.GetTitle()] = PlanConfig{
			ID:         *plan.GetId(),
			Name:       *plan.GetTitle(),
			Buckets:    buckets,
			Priorities: priorities,
		}
	}

	return MSPlannerConfig{Plans: config}, nil
}

func (c MSPlannerClient) GetConfigJSON() (string, error) {
	config, err := c.GetConfig()
	if err != nil {
		return "", err
	}

	b, err := json.Marshal(&config)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func openDataError(err error) error {
	if err == nil {
		return nil
	}

	errorStr := ""
	switch typed := err.(type) {
	case *odataerrors.ODataError:
		errorStr += fmt.Sprintf("error: %s.", typed.Error())
		if terr := typed.GetErrorEscaped(); terr != nil {
			errorStr += fmt.Sprintf("code: %s.", *terr.GetCode())
			errorStr += fmt.Sprintf("msg: %s.", *terr.GetMessage())
		}
	default:
		errorStr += fmt.Sprintf("%T > error: %#v.", err, err)
	}

	return errors.New(errorStr)
}
