package notification

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	commonshttp "github.com/flanksource/commons/http"
	"github.com/flanksource/duty/context"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/mail"
	mcUtils "github.com/flanksource/incident-commander/utils"
)

func setSystemSMTPCredential(ctx context.Context, smtpURL string) (string, error) {
	smtp, err := mail.GetDefaultSMTP(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get default SMTP config: %w", err)
	}

	prefix := fmt.Sprintf("smtp://%s:%s@%s:%d/",
		url.QueryEscape(smtp.Username.ValueStatic),
		url.QueryEscape(smtp.Password.ValueStatic),
		smtp.Host,
		smtp.Port,
	)
	smtpURL = strings.ReplaceAll(smtpURL, api.SystemSMTP, prefix)

	parsedURL, err := url.Parse(smtpURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse SMTP URL: %w", err)
	}

	query := parsedURL.Query()
	query.Set("FromAddress", smtp.FromAddress)
	query.Set("FromName", smtp.FromName)
	parsedURL.RawQuery = query.Encode()

	return parsedURL.String(), nil
}

func firstNonEmpty(props map[string]string, q url.Values, keys ...string) string {
	for _, k := range keys {
		for p, v := range props {
			if strings.EqualFold(k, p) {
				return v
			}
		}
		if v := q.Get(k); v != "" {
			return v
		}
	}
	return ""
}

func sendSMTP(ctx *Context, smtpURL string, data NotificationTemplate) error {
	if strings.HasPrefix(smtpURL, api.SystemSMTP) {
		var err error
		smtpURL, err = setSystemSMTPCredential(ctx.Context, smtpURL)
		if err != nil {
			return err
		}
	}

	data.Message = mcUtils.MarkdownToHTML(data.Message)
	props := GetPropsForService("smtp", data.Properties)
	injectTitleIntoProperties("smtp", data.Title, props)

	parsedURL, err := url.Parse(smtpURL)
	if err != nil {
		return fmt.Errorf("failed to parse SMTP URL: %w", err)
	}

	query := parsedURL.Query()
	to := firstNonEmpty(props, query, "to", "ToAddresses", "ToAddress")
	from := firstNonEmpty(props, query, "from", "FromAddress")
	fromName := firstNonEmpty(props, query, "fromname", "FromName")
	password, _ := parsedURL.User.Password()
	port, _ := strconv.Atoi(parsedURL.Port())
	headerString := props["headers"]

	var conn v1.ConnectionSMTP
	if err := conn.FromURL(smtpURL); err != nil {
		return ctx.Oops().Wrapf(err, "error parsing SMTP URL")
	}
	if from != "" {
		conn.FromAddress = from
	}
	if fromName != "" {
		conn.FromName = fromName
	}

	m := mail.New(strings.Split(to, ","), data.Title, data.Message, `text/html; charset="UTF-8"`).
		SetFrom(conn.FromName, conn.FromAddress).
		SetCredentials(parsedURL.Hostname(), port, parsedURL.User.Username(), password)

	for _, a := range data.Attachments {
		m.AddAttachment(a)
	}

	if headerString != "" {
		headers, err := mcUtils.StringToStringMap(headerString)
		if err != nil {
			return ctx.Oops().Wrapf(err, "error converting headerString[%s] to map", headerString)
		}
		for k, v := range headers {
			m.SetHeader(k, v)
		}
	}
	return m.Send(conn)
}

func sendGenericWebhook(ctx *Context, rawURL string, data NotificationTemplate) error {
	targetURL := strings.TrimPrefix(rawURL, "generic+")
	payload := map[string]string{
		"title":   data.Title,
		"message": data.Message,
	}
	for k, v := range data.Properties {
		payload[k] = v
	}

	resp, err := commonshttp.NewClient().R(ctx.Context).Post(targetURL, payload)
	if err != nil {
		return fmt.Errorf("generic webhook failed: %w", err)
	}
	if !resp.IsOK() {
		body, _ := resp.AsString()
		return fmt.Errorf("generic webhook returned %d: %s", resp.StatusCode, body)
	}
	return nil
}

func serviceFromURL(rawURL string) string {
	if strings.HasPrefix(rawURL, "smtp://") || strings.HasPrefix(rawURL, api.SystemSMTP) {
		return "smtp"
	}
	if strings.HasPrefix(rawURL, "generic+") {
		return "generic"
	}
	if idx := strings.Index(rawURL, "://"); idx > 0 {
		return rawURL[:idx]
	}
	return ""
}

func shoutrrrSendRaw(ctx *Context, celEnv map[string]any, notificationURL string, data NotificationTemplate) (string, error) {
	service := serviceFromURL(notificationURL)

	celEnv["channel"] = service
	templater := ctx.NewStructTemplater(celEnv, "", TemplateFuncs)
	if err := templater.Walk(&data); err != nil {
		return "", fmt.Errorf("error templating notification: %w", err)
	}
	ctx.WithMessage(data.Message)

	switch service {
	case "smtp":
		return "smtp", sendSMTP(ctx, notificationURL, data)
	case "generic":
		return "generic", sendGenericWebhook(ctx, notificationURL, data)
	default:
		return "", fmt.Errorf("unsupported notification URL scheme: %s", service)
	}
}

func shoutrrrSend(ctx *Context, notificationURL string, payload NotificationMessagePayload, properties map[string]string) (string, error) {
	service := serviceFromURL(notificationURL)

	format := "markdown"
	if service == "smtp" {
		format = "email"
	}

	message, err := FormatNotificationMessage(payload, format)
	if err != nil {
		return "", fmt.Errorf("failed to format message: %w", err)
	}

	data := NotificationTemplate{
		Title:      payload.Title,
		Message:    message,
		Properties: properties,
	}
	ctx.WithMessage(data.Message)

	switch service {
	case "smtp":
		return "smtp", sendSMTP(ctx, notificationURL, data)
	case "generic":
		return "generic", sendGenericWebhook(ctx, notificationURL, data)
	default:
		return "", fmt.Errorf("unsupported notification URL scheme: %s", service)
	}
}

func injectTitleIntoProperties(service, title string, properties map[string]string) map[string]string {
	if title == "" || properties == nil {
		return properties
	}

	if service == "smtp" && properties["subject"] == "" {
		properties["subject"] = title
	}

	return properties
}

func GetPropsForService(service string, property map[string]string) map[string]string {
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
