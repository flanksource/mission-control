package v1

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	"github.com/samber/lo"

	"github.com/flanksource/incident-commander/utils"
)

type SMTPTLS string

const (
	EncryptionNone        SMTPTLS = "None"
	EncryptionExplicitTLS SMTPTLS = "ExplicitTLS"
	EncryptionImplicitTLS SMTPTLS = "ImplicitTLS"
	EncryptionAuto        SMTPTLS = "Auto"
)

type SMTPAuth string

const (
	SMTPAuthNone   SMTPAuth = "none"
	SMTPAuthPlain  SMTPAuth = "plain"
	SMTPAuthOAuth2 SMTPAuth = "oauth2"
)

type ConnectionSMTP struct {
	Host     string       `json:"host"`
	Username types.EnvVar `json:"username,omitempty"`
	Password types.EnvVar `json:"password,omitempty"`

	// Default: false
	InsecureTLS bool `json:"insecureTLS,omitempty"`

	// Encryption Method
	// 	Default: auto
	// 	Possible values: None, ExplicitTLS, ImplicitTLS, Auto
	Encryption SMTPTLS `json:"encryption,omitempty"`

	// SMTP server port
	// 	Default: 587
	Port int `json:"port,omitempty"`

	// Email address that the mail are sent from
	FromAddress string `json:"fromAddress"`

	// Name that the mail are sent from
	FromName string `json:"fromName"`

	// List of recipient e-mails
	ToAddresses []string `json:"toAddresses,omitempty"`

	// The subject of the sent mail
	Subject string `json:"subject,omitempty"`

	// Auth - SMTP authentication method
	// Possible values: none, plain, oauth2
	Auth SMTPAuth `json:"auth,omitempty"`

	// Headers for SMTP Server
	Headers map[string]string `json:"headers,omitempty"`
}

func (obj ConnectionSMTP) ToModel() models.Connection {
	dbObj := models.Connection{}
	obj.Auth, _ = lo.Coalesce(obj.Auth, SMTPAuthPlain)
	obj.Encryption, _ = lo.Coalesce(obj.Encryption, "Auto")
	obj.Port, _ = lo.Coalesce(obj.Port, 587)
	dbObj.URL = fmt.Sprintf("smtp://$(username):$(password)@%s:%d/?UseStartTLS=%s&Encryption=%s&Auth=%s&from=%s&to=%s",
		obj.Host,
		obj.Port,
		strconv.FormatBool(obj.InsecureTLS),
		obj.Encryption,
		obj.Auth,
		obj.FromAddress,
		strings.Join(obj.ToAddresses, ","),
	)

	dbObj.Type = models.ConnectionTypeEmail
	dbObj.InsecureTLS = obj.InsecureTLS
	dbObj.Username = obj.Username.String()
	dbObj.Password = obj.Password.String()
	dbObj.Properties = map[string]string{
		"port":     strconv.Itoa(obj.Port),
		"subject":  obj.Subject,
		"from":     obj.FromAddress,
		"to":       strings.Join(obj.ToAddresses, ","),
		"fromname": obj.FromName,
		"headers":  utils.StringMapToString(obj.Headers),
	}
	return dbObj
}

func SMTPConnectionFromModel(dbObj models.Connection) (ConnectionSMTP, error) {
	var err error
	obj := ConnectionSMTP{}
	url, err := url.Parse(dbObj.URL)
	if err != nil {
		return obj, err
	}
	obj.Host = url.Hostname()
	obj.Port, _ = strconv.Atoi(url.Port())
	query := url.Query()
	obj.InsecureTLS, _ = strconv.ParseBool(query.Get("UseStartTLS"))
	obj.Encryption = SMTPTLS(query.Get("Encryption"))
	obj.Auth = SMTPAuth(query.Get("Auth"))

	if obj.Port == 0 {
		if i, err := strconv.Atoi(dbObj.Properties["port"]); err == nil {
			obj.Port = i
		} else {
			obj.Port = 587
		}
	}
	obj.InsecureTLS = dbObj.InsecureTLS
	if err = (&obj.Username).Scan(dbObj.Username); err != nil {
		return obj, err
	}
	if err = (&obj.Password).Scan(dbObj.Password); err != nil {
		return obj, err
	}
	obj.Subject = dbObj.Properties["subject"]
	obj.FromAddress = dbObj.Properties["from"]
	obj.FromName = dbObj.Properties["fromname"]
	obj.ToAddresses = strings.Split(dbObj.Properties["to"], ",")
	obj.Headers, err = utils.StringToStringMap(dbObj.Properties["headers"])
	if err != nil {
		return obj, err
	}
	return obj, nil
}

// FromURL parses an SMTP URL into ConnectionSMTP fields.
// URL format: smtp://user:pass@host:port/?Encryption=...&Auth=...&UseStartTLS=...&from=...&fromName=...&to=...&subject=...
func (c *ConnectionSMTP) FromURL(smtpURL string) error {
	parsed, err := url.Parse(smtpURL)
	if err != nil {
		return err
	}

	c.Username.ValueStatic = parsed.User.Username()
	c.Password.ValueStatic, _ = parsed.User.Password()
	if c.Username.ValueStatic != "" || (c.Username.ValueFrom != nil && !c.Username.IsEmpty()) {
		c.Auth = SMTPAuthPlain
	}

	c.Host = parsed.Hostname()
	c.Port, _ = strconv.Atoi(parsed.Port())
	if c.Port == 0 {
		c.Port = 587
	}

	query := parsed.Query()

	c.Encryption = SMTPTLS(query.Get("Encryption"))
	c.InsecureTLS, _ = strconv.ParseBool(query.Get("UseStartTLS"))
	if auth := SMTPAuth(query.Get("Auth")); auth != "" {
		c.Auth = auth
	}

	if v := query.Get("from"); v != "" {
		c.FromAddress = v
	}
	if v := query.Get("fromName"); v != "" {
		c.FromName = v
	}
	if v := query.Get("to"); v != "" {
		c.ToAddresses = strings.Split(v, ",")
	}
	if v := query.Get("subject"); v != "" {
		c.Subject = v
	}

	if v := query.Get("headers"); v != "" {
		c.Headers, _ = utils.StringToStringMap(v)
	}

	return nil
}

// FromProperties populates fields from a properties map.
// Does not overwrite fields that are already set.
func (c *ConnectionSMTP) FromProperties(props map[string]string) {
	if c.Host == "" {
		c.Host = props["host"]
	}
	if c.Port == 0 {
		if v := props["port"]; v != "" {
			c.Port, _ = strconv.Atoi(v)
		}
	}
	if c.FromAddress == "" {
		c.FromAddress = props["from"]
	}
	if c.FromName == "" {
		c.FromName = props["fromname"]
	}
	if c.Auth == "" {
		c.Auth = SMTPAuth(props["auth"])
	}
	if c.Encryption == "" {
		c.Encryption = SMTPTLS(props["encryptionMethod"])
	}
	if c.Subject == "" {
		c.Subject = props["subject"]
	}
	if c.Headers == nil {
		if v := props["headers"]; v != "" {
			c.Headers, _ = utils.StringToStringMap(v)
		}
	}
	if len(c.ToAddresses) == 0 {
		if v := props["to"]; v != "" {
			c.ToAddresses = strings.Split(v, ",")
		}
	}
}
