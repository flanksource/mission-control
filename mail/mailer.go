package mail

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/emersion/go-message/mail"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/properties"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

type Mail struct {
	to          []string
	from        string
	fromName    string
	subject     string
	body        string
	contentType string
	headers     map[string]string
	host        string
	port        int
	user        string
	password    string
}

func New(to []string, subject, body, contentType string) *Mail {
	return &Mail{
		to:          to,
		subject:     subject,
		body:        body,
		contentType: contentType,
		headers:     make(map[string]string),
	}
}

func (m *Mail) SetFrom(name, email string) *Mail {
	m.fromName = name
	m.from = email
	return m
}

func (m *Mail) SetHeader(key, value string) *Mail {
	m.headers[key] = value
	return m
}

func (m *Mail) SetCredentials(host string, port int, user, password string) *Mail {
	m.host = host
	m.port = port
	m.user = user
	m.password = password
	return m
}

func (m *Mail) buildMessage() ([]byte, error) {
	var buf bytes.Buffer

	var h mail.Header
	h.SetDate(time.Now())
	h.SetSubject(m.subject)
	h.SetAddressList("From", []*mail.Address{{Name: m.fromName, Address: m.from}})

	toAddrs := make([]*mail.Address, len(m.to))
	for i, addr := range m.to {
		toAddrs[i] = &mail.Address{Address: addr}
	}
	h.SetAddressList("To", toAddrs)

	for key, value := range m.headers {
		h.Set(key, value)
	}

	mw, err := mail.CreateWriter(&buf, h)
	if err != nil {
		return nil, err
	}

	var ih mail.InlineHeader
	ih.Set("Content-Type", m.contentType)

	w, err := mw.CreateSingleInline(ih)
	if err != nil {
		return nil, err
	}

	if _, err := io.WriteString(w, m.body); err != nil {
		return nil, err
	}

	if err := w.Close(); err != nil {
		return nil, err
	}

	if err := mw.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (m *Mail) Send(conn v1.ConnectionSMTP) error {
	m.applyDefaults(conn)

	msg, err := m.buildMessage()
	if err != nil {
		return err
	}

	addr := net.JoinHostPort(m.host, strconv.Itoa(m.port))
	tlsConfig := &tls.Config{ServerName: m.host, InsecureSkipVerify: conn.InsecureTLS}

	var client *smtp.Client
	switch conn.Encryption {
	case v1.EncryptionImplicitTLS:
		client, err = smtp.DialTLS(addr, tlsConfig)
		if err != nil {
			return err
		}

	case v1.EncryptionExplicitTLS:
		client, err = smtp.DialStartTLS(addr, tlsConfig)
		if err != nil {
			return err
		}

	default:
		client, err = smtp.Dial(addr)
		if err != nil {
			return err
		}
	}
	defer func() {
		if err := client.Close(); err != nil {
			logger.Errorf("failed to close SMTP client: %v", err)
		}
	}()

	switch conn.Auth {
	case v1.SMTPAuthOAuth2:
		opt := &sasl.OAuthBearerOptions{
			Username: m.user,
			Token:    m.password,
			Host:     m.host,
			Port:     m.port,
		}
		authSession := sasl.NewOAuthBearerClient(opt)
		if err := client.Auth(authSession); err != nil {
			return fmt.Errorf("failed to authenticate with oauth bearer options: %w", err)
		}

	case v1.SMTPAuthPlain:
		// The identity parameter is useful when you want to authenticate as one user
		// but act on behalf of another user. We don't need that
		authzid := ""
		authSession := sasl.NewPlainClient(authzid, m.user, m.password)
		if err := client.Auth(authSession); err != nil {
			return fmt.Errorf("failed to authenticate: %w", err)
		}

	case v1.SMTPAuthNone:
		// do nothing

	default:
		// do nothing
	}

	if properties.On(false, "smtp.debug") {
		client.DebugWriter = os.Stderr
	}

	return client.SendMail(m.from, m.to, bytes.NewReader(msg))
}

func (m *Mail) applyDefaults(conn v1.ConnectionSMTP) {
	if m.host == "" {
		host := conn.Host
		port := conn.Port
		user := conn.Username.ValueStatic
		password := conn.Password.ValueStatic

		if host == "" {
			host = os.Getenv("SMTP_HOST")
			user = os.Getenv("SMTP_USER")
			password = os.Getenv("SMTP_PASSWORD")
			port, _ = strconv.Atoi(os.Getenv("SMTP_PORT"))
		}

		if port == 0 {
			port = 587
		}

		m.host = host
		m.port = port
		m.user = user
		m.password = password
	}

	if m.from == "" {
		m.from = conn.FromAddress
		m.fromName = conn.FromName
	}
}
