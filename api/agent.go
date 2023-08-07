package api

import "github.com/flanksource/duty/models"

type GenerateAgentRequest struct {
	Name       string
	Properties models.PersonProperties
}

type GeneratedAgent struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Password string `json:"password"`
}
