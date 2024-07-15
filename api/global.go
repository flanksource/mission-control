package api

import (
	"github.com/google/uuid"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const UserIDHeaderKey = "X-User-ID"

var (
	BuildVersion string

	SystemUserID      *uuid.UUID
	CanaryCheckerPath string
	ApmHubPath        string

	Namespace            string
	Kubernetes           kubernetes.Interface
	KubernetesRestConfig *rest.Config

	// Full URL of the mission control web UI.
	PublicWebURL string

	// DefaultArtifactConnection is the connection that's used to save all playbook artifacts.
	DefaultArtifactConnection string
)

const (
	PropertyIncidentsDisabled = "incidents.disable"
)
