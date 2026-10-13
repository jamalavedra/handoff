// Package mail sends login links over SMTP, or logs them when SMTP isn't configured.
package mail

import (
	"fmt"
	"log/slog"
	"net"
	"net/mail"
	"net/smtp"
	"net/url"
	"strings"
	"time"
)

type Mailer struct {
	URL  string // smtp://user:pass@host:587
	From string
}

func (m *Mailer) Enabled() bool { return m.URL != "" }

func (m *Mailer) Send(to, subject, body string) error {
	if !m.Enabled() {
		slog.Info("mail (SMTP not configured, logging instead)", "to", to, "subject", subject, "body", body)
		return nil
	}
	u, err := url.Parse(m.URL)
	if err != nil {
		return err
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "587"
	}
	var a smtp.Auth
	if u.User != nil {
		pw, _ := u.User.Password()
		a = smtp.PlainAuth("", u.User.Username(), pw, host)
	}
	from, err := mail.ParseAddress(m.From)
	if err != nil {
		return fmt.Errorf("HANDOFF_MAIL_FROM: %w", err)
	}
	msg := strings.Join([]string{
		"From: " + from.String(),
		"To: " + to,
		"Subject: " + subject,
		"Date: " + time.Now().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"", body,
	}, "\r\n")
	return smtp.SendMail(net.JoinHostPort(host, port), a, from.Address, []string{to}, []byte(msg))
}
