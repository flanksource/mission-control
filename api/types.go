package api

type Incident struct {
}

type Comment struct {
}

type Hypothesis struct {
}

type Responder struct {
}

type IncidentResponders struct {
}

type Person struct {
	Name   string `json:"name,omitempty"`
	Email  string
	Avatar string
	Role   string
}
