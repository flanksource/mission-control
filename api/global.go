package api

import (
	"github.com/google/uuid"
	"k8s.io/client-go/kubernetes"
)

var (
	BuildVersion string

	SystemUserID      *uuid.UUID
	CanaryCheckerPath string
	ApmHubPath        string
	PostgrestURI      string
	ConfigDB          string
	Kubernetes        kubernetes.Interface
	Namespace         string

	// Full URL of the mission control web UI.
	FrontendURL string

	// Full URL of the mission contorl backend.
	PublicURL string

	// DefaultArtifactConnection is the connection that's used to save all playbook artifacts.
	DefaultArtifactConnection string
)

const (
	PropertyIncidentsDisabled = "incidents.disable"
)
