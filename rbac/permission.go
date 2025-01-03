package rbac

import (
	"fmt"
)

type Permission struct {
	Subject string `json:"subject,omitempty"`
	Object  string `json:"object,omitempty"`
	Action  string `json:"action,omitempty"`
	Deny    bool   `json:"deny,omitempty"`
}

func NewPermission(perm []string) Permission {
	return Permission{
		Subject: perm[0],
		Object:  perm[1],
		Action:  perm[2],
		Deny:    perm[3] == "deny",
	}
}

func NewPermissions(perms [][]string) []Permission {
	var arr []Permission

	for _, p := range perms {
		arr = append(arr, NewPermission(p))
	}

	return arr

}

func (p Permission) String() string {
	return fmt.Sprintf("%s on %s (%s)", p.Subject, p.Object, p.Action)
}
