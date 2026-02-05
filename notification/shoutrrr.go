package notification

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	stripmd "github.com/adityathebe/go-strip-markdown/v2"
	"github.com/containrrr/shoutrrr"
	"github.com/containrrr/shoutrrr/pkg/router"
	"github.com/containrrr/shoutrrr/pkg/types"
	"github.com/flanksource/duty/context"

	"github.com/flanksource/incident-commander/api"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/mail"
	mcUtils "github.com/flanksource/incident-commander/utils"
)

// setSystemSMTPCredential modifies the shoutrrrURL to use the system's SMTP credentials.
func setSystemSMTPCredential(ctx context.Context, shoutrrrURL string) (string, error) {
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
	shoutrrrURL = strings.ReplaceAll(shoutrrrURL, api.SystemSMTP, prefix)

	parsedURL, err := url.Parse(shoutrrrURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse shoutrrr URL: %w", err)
	}

	query := parsedURL.Query()
	query.Set("FromAddress", smtp.FromAddress)
	query.Set("FromName", smtp.FromName)
	parsedURL.RawQuery = query.Encode()

	return parsedURL.String(), nil
}

func PrepareShoutrrrRaw(ctx *Context, celEnv map[string]any, shoutrrrURL string, data *NotificationTemplate) (string, string, *router.ServiceRouter, error) {
	if celEnv == nil {
		celEnv = make(map[string]any)
	}

	if data.Properties == nil {
		data.Properties = make(map[string]string)
	}

	if strings.HasPrefix(shoutrrrURL, api.SystemSMTP) {
		var err error
		shoutrrrURL, err = setSystemSMTPCredential(ctx.Context, shoutrrrURL)
		if err != nil {
			return "", "", nil, err
		}
	}

	sender, err := shoutrrr.CreateSender(shoutrrrURL)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to create a shoutrrr sender client: %w", err)
	}

	service, _, err := sender.ExtractServiceName(shoutrrrURL)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to extract service name: %w", err)
	}

	celEnv["channel"] = service
	templater := ctx.NewStructTemplater(celEnv, "", TemplateFuncs)
	if err := templater.Walk(data); err != nil {
		return "", "", nil, fmt.Errorf("error templating notification: %w", err)
	}

	switch service {
	case "smtp":
		data.Message = mcUtils.MarkdownToHTML(data.Message)
		data.Properties["UseHTML"] = "true" // enforce HTML for smtp

	case "telegram":
		data.Properties["ParseMode"] = "MarkdownV2"

	default:
		data.Message = stripmd.StripOptions(data.Message, stripmd.Options{KeepURL: true})
	}

	return service, shoutrrrURL, sender, nil
}

// shoutrrrSendRaw sends a notification and returns the service it sent the notification to
func shoutrrrSendRaw(ctx *Context, celEnv map[string]any, shoutrrrURL string, data NotificationTemplate) (string, error) {
	service, shoutrrrURL, sender, err := PrepareShoutrrrRaw(ctx, celEnv, shoutrrrURL, &data)
	if err != nil {
		return "", err
	}

	ctx.WithMessage(data.Message)

	data.Properties = GetPropsForService(service, data.Properties)
	injectTitleIntoProperties(service, data.Title, data.Properties)

	params := &types.Params{}
	if data.Properties != nil {
		params = (*types.Params)(&data.Properties)
	}

	// NOTE: Until shoutrrr fixes the "UseHTML" props, we'll use the mailer package
	if service == "smtp" {
		parsedURL, err := url.Parse(shoutrrrURL)
		if err != nil {
			return "", fmt.Errorf("failed to parse shoutrrr URL: %w", err)
		}

		firstNonEmpty := func(params *types.Params, q url.Values, keys ...string) string {
			for _, k := range keys {
				for p := range *params {
					if strings.EqualFold(k, p) {
						return (*params)[p]
					}
				}
				if v := q.Get(k); v != "" {
					return v
				}
			}
			return ""
		}

		query := parsedURL.Query()
		var (
			to           = firstNonEmpty(params, query, "to", "ToAddresses", "ToAddress")
			from         = firstNonEmpty(params, query, "from", "FromAddress")
			fromName     = firstNonEmpty(params, query, "fromname", "FromName")
			password, _  = parsedURL.User.Password()
			port, _      = strconv.Atoi(parsedURL.Port())
			headerString = (*params)["headers"]
		)

		// Build ConnectionSMTP from URL
		var conn v1.ConnectionSMTP
		if err := conn.FromURL(shoutrrrURL); err != nil {
			return "", ctx.Oops().Wrapf(err, "error parsing SMTP URL")
		}
		// Override with params if present
		if from != "" {
			conn.FromAddress = from
		}
		if fromName != "" {
			conn.FromName = fromName
		}

		m := mail.New(strings.Split(to, ","), data.Title, data.Message, `text/html; charset="UTF-8"`).
			SetFrom(conn.FromName, conn.FromAddress).
			SetCredentials(parsedURL.Hostname(), port, parsedURL.User.Username(), password)

		if headerString != "" {
			headers, err := mcUtils.StringToStringMap(headerString)
			if err != nil {
				return "", ctx.Oops().Wrapf(err, "error converting headerString[%s] to map", headerString)
			}
			for k, v := range headers {
				m.SetHeader(k, v)
			}
		}
		return service, m.Send(conn)
	}

	sendErrors := sender.Send(data.Message, params)
	for _, err := range sendErrors {
		if err != nil {
			return "", ctx.Oops().Hint(data.Message).Wrapf(err, "error publishing notification (service=%s)", service)
		}
	}

	return service, nil
}

func PrepareShoutrrr(ctx *Context, shoutrrrURL string, payload NotificationMessagePayload, properties map[string]string) (string, string, *router.ServiceRouter, NotificationTemplate, error) {
	if properties == nil {
		properties = make(map[string]string)
	}

	if strings.HasPrefix(shoutrrrURL, api.SystemSMTP) {
		var err error
		shoutrrrURL, err = setSystemSMTPCredential(ctx.Context, shoutrrrURL)
		if err != nil {
			return "", "", nil, NotificationTemplate{}, err
		}
	}

	sender, err := shoutrrr.CreateSender(shoutrrrURL)
	if err != nil {
		return "", "", nil, NotificationTemplate{}, fmt.Errorf("failed to create a shoutrrr sender client: %w", err)
	}

	service, _, err := sender.ExtractServiceName(shoutrrrURL)
	if err != nil {
		return "", "", nil, NotificationTemplate{}, fmt.Errorf("failed to extract service name: %w", err)
	}

	var message string
	switch service {
	case "smtp":
		message, err = FormatNotificationMessage(payload, "email")
		if err != nil {
			return "", "", nil, NotificationTemplate{}, fmt.Errorf("failed to format html message: %w", err)
		}
		properties["UseHTML"] = "true"
	case "telegram":
		message, err = FormatNotificationMessage(payload, "markdown")
		if err != nil {
			return "", "", nil, NotificationTemplate{}, fmt.Errorf("failed to format markdown message: %w", err)
		}
		properties["ParseMode"] = "MarkdownV2"
	default:
		message, err = FormatNotificationMessage(payload, "markdown")
		if err != nil {
			return "", "", nil, NotificationTemplate{}, fmt.Errorf("failed to format markdown message: %w", err)
		}
		message = stripmd.StripOptions(message, stripmd.Options{KeepURL: true})
	}

	data := NotificationTemplate{
		Title:      payload.Title,
		Message:    message,
		Properties: properties,
	}

	return service, shoutrrrURL, sender, data, nil
}

// shoutrrrSend sends a notification and returns the service it sent the notification to
func shoutrrrSend(ctx *Context, shoutrrrURL string, payload NotificationMessagePayload, properties map[string]string) (string, error) {
	service, shoutrrrURL, sender, data, err := PrepareShoutrrr(ctx, shoutrrrURL, payload, properties)
	if err != nil {
		return "", err
	}

	data.Properties = GetPropsForService(service, data.Properties)
	injectTitleIntoProperties(service, data.Title, data.Properties)

	params := &types.Params{}
	if data.Properties != nil {
		params = (*types.Params)(&data.Properties)
	}

	// NOTE: Until shoutrrr fixes the "UseHTML" props, we'll use the mailer package
	if service == "smtp" {
		parsedURL, err := url.Parse(shoutrrrURL)
		if err != nil {
			return "", fmt.Errorf("failed to parse shoutrrr URL: %w", err)
		}

		firstNonEmpty := func(params *types.Params, q url.Values, keys ...string) string {
			for _, k := range keys {
				for p := range *params {
					if strings.EqualFold(k, p) {
						return (*params)[p]
					}
				}
				if v := q.Get(k); v != "" {
					return v
				}
			}
			return ""
		}

		query := parsedURL.Query()
		var (
			to           = firstNonEmpty(params, query, "to", "ToAddresses", "ToAddress")
			from         = firstNonEmpty(params, query, "from", "FromAddress")
			fromName     = firstNonEmpty(params, query, "fromname", "FromName")
			password, _  = parsedURL.User.Password()
			port, _      = strconv.Atoi(parsedURL.Port())
			headerString = (*params)["headers"]
		)

		// Build ConnectionSMTP from URL
		var conn v1.ConnectionSMTP
		if err := conn.FromURL(shoutrrrURL); err != nil {
			return "", ctx.Oops().Wrapf(err, "error parsing SMTP URL")
		}
		// Override with params if present
		if from != "" {
			conn.FromAddress = from
		}
		if fromName != "" {
			conn.FromName = fromName
		}

		m := mail.New(strings.Split(to, ","), data.Title, data.Message, `text/html; charset="UTF-8"`).
			SetFrom(conn.FromName, conn.FromAddress).
			SetCredentials(parsedURL.Hostname(), port, parsedURL.User.Username(), password)

		if headerString != "" {
			headers, err := mcUtils.StringToStringMap(headerString)
			if err != nil {
				return "", ctx.Oops().Wrapf(err, "error converting headerString[%s] to map", headerString)
			}
			for k, v := range headers {
				m.SetHeader(k, v)
			}
		}
		return service, m.Send(conn)
	}

	sendErrors := sender.Send(data.Message, params)
	for _, err := range sendErrors {
		if err != nil {
			return "", ctx.Oops().Hint(data.Message).Wrapf(err, "error publishing notification (service=%s)", service)
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
