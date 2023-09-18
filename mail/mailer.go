package mail

import (
	"os"
	"strconv"

	"gopkg.in/gomail.v2"
)

var FromAddress string

type Mail struct {
	message *gomail.Message
	dialer  *gomail.Dialer
}

func New(to, subject, body, contentType string) *Mail {
	m := gomail.NewMessage()
	m.SetHeader("From", FromAddress)
	m.SetHeader("To", to)
	m.SetHeader("Subject", subject)
	m.SetBody(contentType, body)
	return &Mail{message: m}
}

func (t *Mail) SetFrom(from string) *Mail {
	t.message.SetHeader("From", from)
	return t
}

func (t *Mail) SetCredentials(host string, port int, user, password string) *Mail {
	t.dialer = gomail.NewDialer(host, port, user, password)
	return t
}

func (m Mail) Send() error {
	if m.dialer == nil {
		host := os.Getenv("SMTP_HOST")
		user := os.Getenv("SMTP_USER")
		password := os.Getenv("SMTP_PASSWORD")
		port, _ := strconv.Atoi(os.Getenv("SMTP_PORT"))
		m.SetCredentials(host, port, user, password)
	}

	return m.dialer.DialAndSend(m.message)
}
