package controllers

import (
	"net/smtp"
	"os"
)

// email sender

func (c *Construct) SendEmail(to, subject, body string) error {
	from := "noreply@innovationhub.com"
	smtpHost := "smtp.gmail.com"
	smtpPort := "587"

	auth := smtp.PlainAuth("", from, os.Getenv("EMAIL_PASSWORD"), smtpHost)

	msg := []byte("To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"Content-Type: text/html; charset=\"UTF-8\"\r\n" +
		"\r\n" +
		body + "\r\n")

	err := smtp.SendMail(smtpHost+":"+smtpPort, auth, from, []string{to}, msg)
	if err != nil {
		return err
	}

	return nil
}
