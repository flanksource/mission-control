package db

import (
	"strings"

	"github.com/flanksource/duty/models"
)

// MaskedValue replaces a credential that reading a connection doesn't reveal.
// It's the value duty's connection_details view masks with.
const MaskedValue = "***"

var secretReferencePrefixes = []string{"secret://", "configmap://", "helm://", "serviceaccount://"}

// MaskCredential masks a credential unless it's empty or only references where it's stored, e.g. secret://name/key.
// Reading a connection shows which secret it references, but never the value (specs/authorization/roles.md, Section 2.2).
func MaskCredential(value string) string {
	if value == "" {
		return value
	}
	for _, prefix := range secretReferencePrefixes {
		if strings.HasPrefix(value, prefix) {
			return value
		}
	}
	return MaskedValue
}

// MaskConnection returns the connection as reading it shows it: with its credentials masked, in its username,
// password, certificate, entire URL and all properties. URLs and properties have no reliable secret/settings classification.
func MaskConnection(conn models.Connection) models.Connection {
	conn.Username = MaskCredential(conn.Username)
	conn.Password = MaskCredential(conn.Password)
	conn.Certificate = MaskCredential(conn.Certificate)
	conn.URL = MaskCredential(conn.URL)

	if len(conn.Properties) > 0 {
		properties := make(map[string]string, len(conn.Properties))
		for k, v := range conn.Properties {
			properties[k] = MaskCredential(v)
		}
		conn.Properties = properties
	}

	return conn
}
