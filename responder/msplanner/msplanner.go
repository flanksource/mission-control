package msplanner

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/flanksource/incident-commander/api"
	kiotaAuth "github.com/microsoft/kiota-authentication-azure-go"
	msgraphsdk "github.com/microsoftgraph/msgraph-sdk-go"
	"github.com/microsoftgraph/msgraph-sdk-go/groups/item/threads/item/reply"
	"github.com/microsoftgraph/msgraph-sdk-go/models"
	"github.com/microsoftgraph/msgraph-sdk-go/models/odataerrors"
	"github.com/microsoftgraph/msgraph-sdk-go/planner/tasks/item"
)

type MSPlannerTask struct {
	Title       string
	PlanID      string
	Priority    int32
	Description string
	BucketID    string
}

type PlanConfig struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Buckets    []PlanBucket   `json:"buckets"`
	Priorities map[string]int `json:"priorities"`
}

type PlanBucket struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type MSPlannerClient struct {
	client  *msgraphsdk.GraphServiceClient
	groupID string
}

func NewClient() (MSPlannerClient, error) {
	cred, err := azidentity.NewDeviceCodeCredential(&azidentity.DeviceCodeCredentialOptions{
		ClientID: "CLIENT_ID",
		UserPrompt: func(ctx context.Context, message azidentity.DeviceCodeMessage) error {
			fmt.Println(message.Message)
			return nil
		},
	})
	if err != nil {
		return MSPlannerClient{}, fmt.Errorf("Error creating credentials: %v\n", err)
	}

	auth, err := kiotaAuth.NewAzureIdentityAuthenticationProviderWithScopes(cred, []string{"User.Read"})
	if err != nil {
		return MSPlannerClient{}, fmt.Errorf("Error authentication provider: %v\n", err)
	}

	adapter, err := msgraphsdk.NewGraphRequestAdapter(auth)
	if err != nil {
		return MSPlannerClient{}, fmt.Errorf("Error creating adapter: %v\n", err)
	}
	client := msgraphsdk.NewGraphServiceClient(adapter)
	return MSPlannerClient{client: client}, nil
}

func (c MSPlannerClient) CreateTask(opts MSPlannerTask) (models.PlannerTaskable, error) {
	body := models.NewPlannerTask()

	// body.SetPriority()
	// TODO: Description is a hell-hole
	// Use models.PLannerTaskDetails and then set
	body.SetPlanId(&opts.PlanID)
	body.SetBucketId(&opts.BucketID)
	body.SetTitle(&opts.Title)
	body.SetPriority(&opts.Priority)

	result, err := c.client.Planner().Tasks().Post(body)
	return result, openDataError(err)
}

func (c MSPlannerClient) AddComment(taskID, comment string) error {

	// First check if conversation thread already exists for not, if not create new
	// Get task
	// if task.GetConvoId == nil

	task, err := c.client.Planner().TasksById(taskID).Get()
	if err != nil {
		return openDataError(err)
	}

	post := getCommentPost(comment)

	// Create a new conversation thread if task does not has one
	if task.GetConversationThreadId() == nil {
		convBody := models.NewConversationThread()
		topic := fmt.Sprintf("Conversation thread topic for taskID: %s", taskID)
		convBody.SetTopic(&topic)

		convBody.SetPosts([]models.Postable{post})

		result, err := c.client.GroupsById(c.groupID).Threads().Post(convBody)
		if err != nil {
			fmt.Println("error creating Conversation thread")
			return openDataError(err)
		}

		// TODO: For debugging
		fmt.Println("Conversation thread created")
		fmt.Println(result)

		// TODO: Link conversation thread to task
		etag := *task.GetAdditionalData()["@odata.etag"].(*string)
		headers := map[string]string{"If-Match": etag}
		patchConfig := item.PlannerTaskItemRequestBuilderPatchRequestConfiguration{Headers: headers}

		requestBody := models.NewPlannerTask()
		requestBody.SetConversationThreadId(result.GetId())
		err = c.client.Planner().TasksById(taskID).PatchWithRequestConfigurationAndResponseHandler(requestBody, &patchConfig, nil)
		// TODO: For debugging
		if err != nil {
			fmt.Println("error setting Conversation thread")
		}
		return openDataError(err)

	}

	// Use reply package like items
	replyBody := reply.NewReplyPostRequestBody()
	replyBody.SetPost(post)

	err = c.client.GroupsById(c.groupID).ThreadsById(*task.GetConversationThreadId()).Reply().Post(replyBody)
	return openDataError(err)
}

func (c MSPlannerClient) GetComments(taskID string) ([]api.Comment, error) {
	task, err := c.client.Planner().TasksById(taskID).Get()
	if err != nil {
		return nil, openDataError(err)
	}
	conversations, err := c.client.GroupsById(c.groupID).ThreadsById(*task.GetConversationThreadId()).Posts().Get()
	if err != nil {
		return nil, openDataError(err)
	}

	var comments []api.Comment
	for _, conv := range conversations.GetValue() {
		comments = append(comments, api.Comment{
			Body:      *conv.GetBody().GetContent(),
			CreatedBy: *conv.GetFrom().GetEmailAddress().GetName(),
			CreatedAt: *conv.GetCreatedDateTime(),
		})
	}

	return comments, nil
}

func (c MSPlannerClient) GetConfig() (map[string]PlanConfig, error) {
	// TODO: List all available plans
	// Get their buckets
	// Priorities can be hard-coded
	config := make(map[string]PlanConfig)
	return config, nil
}

func getCommentPost(comment string) *models.Post {
	post := models.NewPost()
	body := models.NewItemBody()
	contentType := models.TEXT_BODYTYPE
	body.SetContentType(&contentType)
	body.SetContent(&comment)
	post.SetBody(body)
	return post
}

func openDataError(err error) error {
	if err == nil {
		return nil
	}

	errorStr := ""
	switch err.(type) {
	case *odataerrors.ODataError:
		typed := err.(*odataerrors.ODataError)
		errorStr += fmt.Sprintf("error: %s.", typed.Error())
		if terr := typed.GetError(); terr != nil {
			errorStr += fmt.Sprintf("code: %s.", *terr.GetCode())
			errorStr += fmt.Sprintf("msg: %s.", *terr.GetMessage())
		}
	default:
		errorStr += fmt.Sprintf("%T > error: %#v.", err, err)
	}

	return fmt.Errorf(errorStr)
}
