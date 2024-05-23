package api

import (
	"github.com/google/uuid"
	"k8s.io/client-go/kubernetes"
)

const UserIDHeaderKey = "X-User-ID"

var (
	BuildVersion string

	SystemUserID      *uuid.UUID
	CanaryCheckerPath string
	ApmHubPath        string
	Kubernetes        kubernetes.Interface
	Namespace         string

	// Full URL of the mission control web UI.
	PublicWebURL string

	// DefaultArtifactConnection is the connection that's used to save all playbook artifacts.
	DefaultArtifactConnection string
)

const (
	PropertyIncidentsDisabled = "incidents.disable"
)
