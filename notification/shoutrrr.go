package notification

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/containrrr/shoutrrr"
	"github.com/containrrr/shoutrrr/pkg/types"
	"github.com/flanksource/commons/collections"
	"github.com/flanksource/incident-commander/api"
)

// defaultSMTPPrefix indicates that the shoutrrr URL for smtp should use
// the system's SMTP credentials.
const defaultSMTPPrefix = "smtp://auto:auto@auto/"

// setSystemSMTPCredential modifies the shoutrrrURL to use the system's SMTP credentials.
func setSystemSMTPCredential(shoutrrrURL string) (string, error) {
	prefix := fmt.Sprintf("smtp://%s:%s@%s:%s/",
		url.QueryEscape(os.Getenv("SMTP_USER")),
		url.QueryEscape(os.Getenv("SMTP_PASSWORD")),
		os.Getenv("SMTP_HOST"),
		os.Getenv("SMTP_PORT"),
	)
	shoutrrrURL = strings.ReplaceAll(shoutrrrURL, defaultSMTPPrefix, prefix)

	parsedURL, err := url.Parse(shoutrrrURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse shoutrrr URL: %w", err)
	}

	query := parsedURL.Query()
	query.Set("FromAddress", os.Getenv("SMTP_USER"))
	parsedURL.RawQuery = query.Encode()

	shoutrrrURL = parsedURL.String()
	return shoutrrrURL, nil
}

func Send(ctx *api.Context, connectionName, shoutrrrURL, message string, properties ...map[string]string) error {
	if connectionName != "" {
		connection, err := ctx.HydrateConnection(connectionName)
		if err != nil {
			return err
		}

		shoutrrrURL = connection.URL
		properties = append([]map[string]string{connection.Properties}, properties...)
	}

	if strings.HasPrefix(shoutrrrURL, defaultSMTPPrefix) {
		var err error
		shoutrrrURL, err = setSystemSMTPCredential(shoutrrrURL)
		if err != nil {
			return err
		}
	}

	sender, err := shoutrrr.CreateSender(shoutrrrURL)
	if err != nil {
		return fmt.Errorf("failed to create a shoutrrr sender client: %w", err)
	}

	service, _, err := sender.ExtractServiceName(shoutrrrURL)
	if err != nil {
		return fmt.Errorf("failed to extract service name: %w", err)
	}

	var allProps map[string]string
	for _, prop := range properties {
		prop = getPropsForService(service, prop)
		allProps = collections.MergeMap(allProps, prop)
	}

	var params *types.Params
	if properties != nil {
		params = (*types.Params)(&allProps)
	}

	sendErrors := sender.Send(message, params)
	for _, err := range sendErrors {
		if err != nil {
			return fmt.Errorf("error publishing notification: %w", err)
		}
	}

	return nil
}

func getPropsForService(service string, property map[string]string) map[string]string {
	if service == "smtp" {
		service = "email"
	}

	output := make(map[string]string, len(property))
	for k, v := range property {
		if !strings.Contains(k, ".") {
			output[k] = v
		}

		if after, found := strings.CutPrefix(k, service+"."); found {
			output[after] = v
		}
	}

	return output
}
