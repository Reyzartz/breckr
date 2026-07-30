package notifier

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"

	"breckr-server/internal/types"
)

// Email delivers over SMTP, defaulting to Gmail.
//
// Gmail requires an app password rather than the account password, which the
// spec's validation message says outright -- it is the single most common way
// this channel fails to work.
type Email struct {
	spec   *EmailSpec
	logger *log.Logger
	// addr overrides host:port. Tests only, to point at a local SMTP stub.
	addr string
	// sendMail is the injection seam for tests, standing in for the real
	// dial-and-send below.
	sendMail func(addr string, auth smtp.Auth, from string, to []string, body []byte) error
}

func NewEmail(spec *EmailSpec, logger *log.Logger) *Email {
	return &Email{spec: spec, logger: logger, sendMail: sendMailSTARTTLS}
}

// Send builds an RFC 5322 message and hands it to SMTP.
//
// net/smtp predates context, so the send runs on its own goroutine and the
// context race decides what the caller hears. The goroutine is left to finish on
// its own rather than leaked indefinitely: the dialer and client both carry
// their own timeouts, so it cannot outlive them.
func (e *Email) Send(ctx context.Context, message Message) types.NotificationOutcome {
	addr := e.addr
	if addr == "" {
		addr = net.JoinHostPort(e.spec.host(), fmt.Sprint(e.spec.port()))
	}

	recipients := e.spec.recipients()
	body := buildMessage(e.spec.from(), recipients, message)
	auth := smtp.PlainAuth("", e.spec.Username, e.spec.AppPassword, e.spec.host())

	ctx, cancel := context.WithTimeout(ctx, types.NotifyTimeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- e.sendMail(addr, auth, e.spec.from(), recipients, body)
	}()

	select {
	case err := <-done:
		if err != nil {
			return fail(e.logger,
				"failed to send the email via %s -- the notification will be retried on the next run: %v",
				addr, err)
		}
		return delivered()

	case <-ctx.Done():
		return fail(e.logger, "sending the email via %s timed out after %s", addr, types.NotifyTimeout)
	}
}

// sendMailSTARTTLS is smtp.SendMail with a bounded dial.
//
// smtp.SendMail dials with no timeout at all, so an unreachable host would hang
// until the OS gave up -- well past the point the run should have moved on.
func sendMailSTARTTLS(addr string, auth smtp.Auth, from string, to []string, body []byte) error {
	dialer := net.Dialer{Timeout: types.NotifyTimeout}
	connection, err := dialer.Dial("tcp", addr)
	if err != nil {
		return err
	}

	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		connection.Close()
		return err
	}

	// Bounds every subsequent read and write on the conversation, not just the
	// dial.
	_ = connection.SetDeadline(time.Now().Add(types.NotifyTimeout))

	client, err := smtp.NewClient(connection, host)
	if err != nil {
		connection.Close()
		return err
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return err
		}
	}

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return err
		}
	}

	if err := client.Mail(from); err != nil {
		return err
	}
	for _, recipient := range to {
		if err := client.Rcpt(recipient); err != nil {
			return err
		}
	}

	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(body); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	return client.Quit()
}

// buildMessage assembles the RFC 5322 body.
//
// The subject is RFC 2047 encoded so a non-ASCII task name survives the trip,
// and the body is CRLF-terminated because SMTP requires it -- a bare \n makes
// some servers reject the message.
func buildMessage(from string, to []string, message Message) []byte {
	subject := message.Subject
	if subject == "" {
		subject = types.DefaultEmailSubject
	}

	headers := []string{
		"From: " + from,
		"To: " + strings.Join(to, ", "),
		"Subject: " + mime.QEncoding.Encode("utf-8", subject),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
	}

	body := strings.ReplaceAll(message.Body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\n", "\r\n")

	return []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + body + "\r\n")
}

var _ Transport = (*Email)(nil)
