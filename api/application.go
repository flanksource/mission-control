package api

import "time"

// Application is the schema that UI uses.
type Application struct {
	ApplicationDetail `json:",inline"`
	AccessControl     ApplicationAccessControl `json:"accessControl"`
}

type ApplicationAccessControl struct {
	Users          []UserAndRole `json:"users"`
	Authentication []AuthMethod  `json:"authentication"`
}

type ApplicationDetail struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	Namespace   string    `json:"namespace"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

type UserAndRole struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Email            string     `json:"email"`
	Role             string     `json:"role"`
	AuthType         string     `json:"authType"`
	CreatedAt        time.Time  `json:"created"`
	LastLogin        *time.Time `json:"lastLogin,omitempty"`
	LastAccessReview *time.Time `json:"lastAccessReview,omitempty"`
}

type AuthMethod struct {
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Properties map[string]string `json:"properties"`
}
