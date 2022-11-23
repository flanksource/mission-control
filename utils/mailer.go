package utils

import (
	"os"
	"strconv"

	"gopkg.in/gomail.v2"
)

func NewMail(from, to, subject, body, contentType string) *gomail.Message {
	m := gomail.NewMessage()
	m.SetHeader("From", from)
	m.SetHeader("To", to)
	m.SetHeader("Subject", subject)
	m.SetBody(contentType, body)
	return m
}

func SendMail(mail *gomail.Message) error {
	host := os.Getenv("SMTP_HOST")
	user := os.Getenv("SMTP_USER")
	password := os.Getenv("SMTP_PASSWORD")
	port, _ := strconv.Atoi(os.Getenv("SMTP_PORT"))
	d := gomail.NewDialer(host, port, user, password)
	return d.DialAndSend(mail)
}
