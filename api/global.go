package api

import (
	"github.com/google/uuid"
)

var (
	BuildVersion string

	SystemUserID      *uuid.UUID
	CanaryCheckerPath string
	ApmHubPath        string
	ConfigDB          string
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

type LLMBackend string

const (
	LLMBackendAnthropic LLMBackend = "anthropic"
	LLMBackendOpenAI    LLMBackend = "openai"
	LLMBackendOllama    LLMBackend = "ollama"
	LLMBackendGemini    LLMBackend = "gemini"
)
