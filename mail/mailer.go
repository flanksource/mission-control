package mail

import (
	"os"
	"strconv"

	"gopkg.in/gomail.v2"
)

var FromAddress string

type Mail struct {
	message *gomail.Message
}

func (t *Mail) SetFrom(from string) *Mail {
	t.message.SetHeader("From", from)
	return t
}

func New(to, subject, body, contentType string) *Mail {
	m := gomail.NewMessage()
	m.SetHeader("From", FromAddress)
	m.SetHeader("To", to)
	m.SetHeader("Subject", subject)
	m.SetBody(contentType, body)
	return &Mail{message: m}
}

func (m Mail) Send() error {
	host := os.Getenv("SMTP_HOST")
	user := os.Getenv("SMTP_USER")
	password := os.Getenv("SMTP_PASSWORD")
	port, _ := strconv.Atoi(os.Getenv("SMTP_PORT"))
	d := gomail.NewDialer(host, port, user, password)
	return d.DialAndSend(m.message)
}
