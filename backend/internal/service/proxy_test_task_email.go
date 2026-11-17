package service

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"time"
)

// SendEmailContext uses the existing SMTP configuration/message format while
// allowing a scheduled task's cancellation to close every SMTP network phase.
func (s *EmailService) SendEmailContext(ctx context.Context, to, subject, body string) error {
	config, err := s.GetSMTPConfig(ctx)
	if err != nil {
		return err
	}
	message, err := buildSMTPMessage(config, to, subject, body)
	if err != nil {
		return err
	}
	client, release, err := connectProxyTaskSMTP(ctx, config)
	if err != nil {
		return err
	}
	defer release()
	defer func() { _ = client.Close() }()
	if err := client.Auth(smtp.PlainAuth("", config.Username, config.Password, config.Host)); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err := client.Mail(message.envelopeFrom); err != nil {
		return fmt.Errorf("smtp mail: %w", err)
	}
	if err := client.Rcpt(message.envelopeTo); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write(message.data); err != nil {
		return fmt.Errorf("write msg: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close writer: %w", err)
	}
	_ = client.Quit() // Acceptance occurred at DATA completion, as in SendEmailWithConfig.
	return nil
}

func connectProxyTaskSMTP(ctx context.Context, config *SMTPConfig) (*smtp.Client, func(), error) {
	dial := func() (net.Conn, func(), error) {
		dialer := &net.Dialer{Timeout: smtpDialTimeout}
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(config.Host, strconv.Itoa(config.Port)))
		if err != nil {
			return nil, nil, err
		}
		deadline := time.Now().Add(smtpIOTimeout)
		if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
			deadline = ctxDeadline
		}
		_ = conn.SetDeadline(deadline)
		stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
		return conn, func() { stop(); _ = conn.Close() }, nil
	}
	conn, release, err := dial()
	if err != nil {
		return nil, nil, fmt.Errorf("smtp dial: %w", err)
	}
	if config.UseTLS {
		tlsConn := tls.Client(conn, smtpTLSConfig(config.Host))
		if err := tlsConn.HandshakeContext(ctx); err == nil {
			client, err := smtp.NewClient(tlsConn, config.Host)
			if err != nil {
				release()
				return nil, nil, err
			}
			return client, release, nil
		} else {
			release()
			var recordErr tls.RecordHeaderError
			if !errors.As(err, &recordErr) {
				return nil, nil, fmt.Errorf("tls dial: %w", err)
			}
		}
		// Match existing SMTP behavior for STARTTLS submission ports.
		conn, release, err = dial()
		if err != nil {
			return nil, nil, err
		}
	}
	client, err := smtp.NewClient(conn, config.Host)
	if err != nil {
		release()
		return nil, nil, err
	}
	if supported, _ := client.Extension("STARTTLS"); supported {
		if err := client.StartTLS(smtpTLSConfig(config.Host)); err != nil {
			release()
			return nil, nil, fmt.Errorf("starttls: %w", err)
		}
	} else if config.UseTLS {
		release()
		return nil, nil, errors.New("smtp server does not support STARTTLS")
	}
	return client, release, nil
}
