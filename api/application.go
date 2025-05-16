package api

import "time"

// Application is the schema that UI uses.
type Application struct {
	Details      ApplicationDetail `json:"details"`
	UserAndRoles []UserAndRole     `json:"userAndRoles"`
	AuthMethods  []AuthMethod      `json:"authMethods"`
}

type ApplicationDetail struct {
	Name        string    `json:"name"`
	Namespace   string    `json:"namespace"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

type UserAndRole struct {
	Name             string     `json:"name"`
	Email            string     `json:"email"`
	Roles            []string   `json:"roles"`
	AuthType         string     `json:"authType"`
	CreatedAt        time.Time  `json:"createdAt"`
	LastLogin        *time.Time `json:"lastLogin,omitempty"`
	LastAccessReview *time.Time `json:"lastAccessReview,omitempty"`
}

type AuthMethod struct {
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Properties map[string]string `json:"properties"`
}
