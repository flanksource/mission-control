package api

import (
	"github.com/flanksource/kommons"
	"github.com/google/uuid"
	"k8s.io/client-go/kubernetes"
)

const UserIDHeaderKey = "X-User-ID"

var (
	SystemUserID      *uuid.UUID
	CanaryCheckerPath string
	ApmHubPath        string
	Kubernetes        kubernetes.Interface
	KommonsClient     *kommons.Client
	Namespace         string

	// Full URL of the mission control web UI.
	PublicWebURL string
)
