package api

type NotificationConfig struct {
	Filter     string            `json:"filter,omitempty"`                     // Filter is a CEL-expression used to decide whether this notification client should send the notification
	URL        string            `json:"url,omitempty"`                        // URL in the form of Shoutrrr notification service URL schema
	Template   string            `json:"template"`                             // Go template for the notification message
	Connection string            `json:"connection,omitempty"`                 // Connection is the name of the connection
	Properties map[string]string `json:"properties,omitempty" template:"true"` // Configuration properties for Shoutrrr. It's Templatable.
}

func (t *NotificationConfig) HydrateConnection(ctx *Context) error {
	connection, err := ctx.HydrateConnection(t.Connection)
	if err != nil {
		return err
	} else if connection == nil {
		return nil
	}

	t.URL = connection.URL

	return nil
}
