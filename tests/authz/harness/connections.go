package harness

import "github.com/google/uuid"

// Connection is a connection, created and changed through /connection.
type Connection struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Type      string `json:"type"`
	URL       string `json:"url"`
	Username  string `json:"username"`
	Password  string `json:"password"`

	// secret is the password it was created with: reading it back shows the password masked
	secret string
}

// ScopeType implements Resource.
func (*Connection) ScopeType() string { return "connection" }

// Prepare implements Resource. A connection is an HTTP connection with a password of its own unless it says otherwise.
func (c *Connection) Prepare(f *Fixture) {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	if c.Name == "" {
		c.Name = f.Name("conn")
	}
	if c.Namespace == "" {
		c.Namespace = f.Namespace
	}
	if c.Type == "" {
		c.Type = "http"
	}
	if c.Username == "" {
		c.Username = "authz-user"
	}
	if c.Password == "" {
		c.Password = f.Secret()
	}
	c.secret = c.Password
}

// CreateRequest implements Resource.
func (c *Connection) CreateRequest() Request {
	return POST("/connection", c.Body())
}

// Path implements Resource.
func (c *Connection) Path() string { return "/connection/" + c.ID }

// Secret implements Secretive.
func (c *Connection) Secret() string { return c.secret }

// Body is the connection as it's sent to create or save one.
func (c *Connection) Body() map[string]any {
	return map[string]any{
		"id": c.ID, "name": c.Name, "namespace": c.Namespace, "type": c.Type,
		"url": c.URL, "username": c.Username, "password": c.Password,
	}
}

// With returns a copy of the connection changed by change, e.g. to send as an update.
func (c *Connection) With(change func(*Connection)) *Connection {
	changed := *c
	change(&changed)
	return &changed
}
