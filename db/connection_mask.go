package db

import (
	"strings"

	"github.com/flanksource/duty/models"
)

// MaskedValue replaces a credential that reading a connection doesn't reveal.
// It's the value duty's connection_details view masks with, and that its update trigger keeps the stored value for.
const MaskedValue = "***"

// credentialProperties are the connection properties, normalized by normalizePropertyKey, that hold credentials.
var credentialProperties = map[string]struct{}{
	"accesstoken":    {},
	"apikey":         {},
	"awssigv4":       {},
	"bearer":         {},
	"clientsecret":   {},
	"cookies":        {},
	"headers":        {},
	"key":            {},
	"password":       {},
	"privatekey":     {},
	"refreshtoken":   {},
	"secret":         {},
	"secretkey":      {},
	"sessionstorage": {},
	"sessiontoken":   {},
	"storagestate":   {},
	"token":          {},
}

var secretReferencePrefixes = []string{"secret://", "configmap://", "helm://", "serviceaccount://"}

func normalizePropertyKey(key string) string {
	return strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
}

// IsCredentialProperty reports whether a connection property holds a credential.
func IsCredentialProperty(key string) bool {
	_, ok := credentialProperties[normalizePropertyKey(key)]
	return ok
}

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

// MaskConnectionURL masks a password written into a connection's URL, e.g. postgres://user:password@host.
// Templates such as $(password), filled in from the connection's own fields, are kept.
func MaskConnectionURL(raw string) string {
	scheme, rest, ok := strings.Cut(raw, "://")
	if !ok {
		return raw
	}

	authority := rest
	if end := strings.IndexAny(rest, "/?#"); end >= 0 {
		authority = rest[:end]
	}

	at := strings.LastIndex(authority, "@")
	if at < 0 {
		return raw
	}

	user, password, ok := strings.Cut(authority[:at], ":")
	if !ok || password == "" || strings.Contains(password, "$(") || MaskCredential(password) == password {
		return raw
	}

	return scheme + "://" + user + ":" + MaskedValue + rest[at:]
}

// MaskConnection returns the connection as reading it shows it: with its credentials masked, in its username,
// password, certificate, URL and credential properties.
func MaskConnection(conn models.Connection) models.Connection {
	conn.Username = MaskCredential(conn.Username)
	conn.Password = MaskCredential(conn.Password)
	conn.Certificate = MaskCredential(conn.Certificate)
	conn.URL = MaskConnectionURL(conn.URL)

	if len(conn.Properties) > 0 {
		properties := make(map[string]string, len(conn.Properties))
		for k, v := range conn.Properties {
			if IsCredentialProperty(k) {
				v = MaskCredential(v)
			}
			properties[k] = v
		}
		conn.Properties = properties
	}

	return conn
}

// MaskConnectionRow masks the credentials of a connection row as decoded from JSON, e.g. a row PostgREST returns.
func MaskConnectionRow(row map[string]any) {
	for _, field := range []string{"username", "password", "certificate"} {
		if v, ok := row[field].(string); ok {
			row[field] = MaskCredential(v)
		}
	}

	if v, ok := row["url"].(string); ok {
		row["url"] = MaskConnectionURL(v)
	}

	if properties, ok := row["properties"].(map[string]any); ok {
		for k, v := range properties {
			if !IsCredentialProperty(k) {
				continue
			}
			if s, ok := v.(string); ok {
				properties[k] = MaskCredential(s)
			} else if v != nil {
				properties[k] = MaskedValue
			}
		}
	}
}

// UnmaskConnectionRow puts back the stored credentials a write of a connection row sends masked, as read.
// A client that reads a connection and saves it back would otherwise overwrite its credentials with the mask.
func UnmaskConnectionRow(row map[string]any, stored models.Connection) {
	for field, value := range map[string]string{"username": stored.Username, "password": stored.Password, "certificate": stored.Certificate} {
		if row[field] == MaskedValue {
			row[field] = value
		}
	}

	if v, ok := row["url"].(string); ok && v != stored.URL && v == MaskConnectionURL(stored.URL) {
		row["url"] = stored.URL
	}

	if properties, ok := row["properties"].(map[string]any); ok {
		for k, v := range properties {
			if stored, exists := stored.Properties[k]; exists && v == MaskedValue {
				properties[k] = stored
			}
		}
	}
}
