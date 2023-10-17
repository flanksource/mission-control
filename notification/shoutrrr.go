package notification

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	stripmd "github.com/adityathebe/go-strip-markdown/v2"
	"github.com/containrrr/shoutrrr"
	"github.com/containrrr/shoutrrr/pkg/types"
	"github.com/flanksource/commons/collections"
	"github.com/flanksource/commons/utils"
	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/mail"
	icUtils "github.com/flanksource/incident-commander/utils"
)

// setSystemSMTPCredential modifies the shoutrrrURL to use the system's SMTP credentials.
func setSystemSMTPCredential(shoutrrrURL string) (string, error) {
	prefix := fmt.Sprintf("smtp://%s:%s@%s:%s/",
		url.QueryEscape(os.Getenv("SMTP_USER")),
		url.QueryEscape(os.Getenv("SMTP_PASSWORD")),
		os.Getenv("SMTP_HOST"),
		os.Getenv("SMTP_PORT"),
	)
	shoutrrrURL = strings.ReplaceAll(shoutrrrURL, api.SystemSMTP, prefix)

	parsedURL, err := url.Parse(shoutrrrURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse shoutrrr URL: %w", err)
	}

	query := parsedURL.Query()
	query.Set("FromAddress", mail.FromAddress)
	query.Set("FromName", mail.FromName)
	parsedURL.RawQuery = query.Encode()

	shoutrrrURL = parsedURL.String()
	return shoutrrrURL, nil
}

func Send(ctx *Context, connectionName, shoutrrrURL, title, message string, properties ...map[string]string) error {
	start := time.Now()

	service, err := send(ctx, connectionName, shoutrrrURL, title, message, properties...)
	if err != nil {
		notificationSendFailureCounter.WithLabelValues(service, string(ctx.recipientType), ctx.notificationID.String()).Inc()
		return err
	}

	notificationSentCounter.WithLabelValues(service, string(ctx.recipientType), ctx.notificationID.String()).Inc()
	notificationSendDuration.WithLabelValues(service, string(ctx.recipientType), ctx.notificationID.String()).Observe(time.Since(start).Seconds())

	return nil
}

// send sends a notification and returns the service it sent the notification to
func send(ctx *Context, connectionName, shoutrrrURL, title, message string, properties ...map[string]string) (string, error) {
	if connectionName != "" {
		connection, err := ctx.HydratedConnectionByURL(api.Namespace, connectionName)
		if err != nil {
			return "", err
		}

		shoutrrrURL = connection.URL
		properties = append([]map[string]string{connection.Properties}, properties...)
	}

	if strings.HasPrefix(shoutrrrURL, api.SystemSMTP) {
		var err error
		shoutrrrURL, err = setSystemSMTPCredential(shoutrrrURL)
		if err != nil {
			return "", err
		}
	}

	sender, err := shoutrrr.CreateSender(shoutrrrURL)
	if err != nil {
		return "", fmt.Errorf("failed to create a shoutrrr sender client: %w", err)
	}

	service, _, err := sender.ExtractServiceName(shoutrrrURL)
	if err != nil {
		return "", fmt.Errorf("failed to extract service name: %w", err)
	}

	switch service {
	case "smtp":
		message = icUtils.MarkdownToHTML(message)
		properties = append(properties, map[string]string{"UseHTML": "true"}) // enforce HTML for smtp

	case "telegram":
		properties = append(properties, map[string]string{"ParseMode": "MarkdownV2"})

	default:
		message = stripmd.StripOptions(message, stripmd.Options{KeepURL: true})
	}

	ctx.WithMessage(message)

	var allProps map[string]string
	for _, prop := range properties {
		prop = getPropsForService(service, prop)
		allProps = collections.MergeMap(allProps, prop)
	}

	injectTitleIntoProperties(service, title, allProps)

	params := &types.Params{}
	if properties != nil {
		params = (*types.Params)(&allProps)
	}

	// NOTE: Until shoutrrr fixes the "UseHTML" props, we'll use the mailer package
	if service == "smtp" {
		parsedURL, err := url.Parse(shoutrrrURL)
		if err != nil {
			return "", fmt.Errorf("failed to parse shoutrrr URL: %w", err)
		}

		query := parsedURL.Query()
		var (
			to          = utils.Coalesce(query.Get("ToAddresses"), (*params)["ToAddresses"])
			from        = utils.Coalesce(query.Get("FromAddress"), (*params)["FromAddress"])
			fromName    = utils.Coalesce(query.Get("FromName"), (*params)["FromName"])
			password, _ = parsedURL.User.Password()
			port, _     = strconv.Atoi(parsedURL.Port())
		)

		m := mail.New(to, title, message, `text/html; charset="UTF-8"`).
			SetFrom(fromName, from).
			SetCredentials(parsedURL.Hostname(), port, parsedURL.User.Username(), password)
		return service, m.Send()
	}

	sendErrors := sender.Send(message, params)
	for _, err := range sendErrors {
		if err != nil {
			return "", fmt.Errorf("error publishing notification (service=%s): %w", service, err)
		}
	}

	return service, nil
}

// injectTitleIntoProperties adds the given title to the shoutrrr properties if it's not already set.
func injectTitleIntoProperties(service, title string, properties map[string]string) map[string]string {
	if title == "" {
		return properties
	}

	switch strings.ToLower(service) {
	case "smtp":
		if properties["subject"] == "" {
			properties["subject"] = title
		}

	case "googlechat", "rocketchat":
		// Do nothing

	default:
		if properties["title"] == "" {
			properties["title"] = title
		}
	}

	return properties
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
