// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

// startFakeSMTP runs a minimal single-connection SMTP conversation on a
// loopback listener. It advertises AUTH PLAIN (loopback plaintext auth
// is permitted by net/smtp) and, when rejectMailFrom is true, answers
// MAIL FROM with a 550 so the send fails after the handshake.
func startFakeSMTP(t *testing.T, rejectMailFrom bool) (host string, port int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		br := bufio.NewReader(conn)
		write := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
		write("220 test ESMTP")
		inData := false
		for {
			line, readErr := br.ReadString('\n')
			if readErr != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if inData {
				if line == "." {
					inData = false
					write("250 OK")
				}
				continue
			}
			cmd := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				write("250-test")
				write("250 AUTH PLAIN")
			case strings.HasPrefix(cmd, "AUTH"):
				write("235 ok")
			case strings.HasPrefix(cmd, "MAIL FROM"):
				if rejectMailFrom {
					write("550 rejected")
				} else {
					write("250 OK")
				}
			case strings.HasPrefix(cmd, "RCPT TO"):
				write("250 OK")
			case strings.HasPrefix(cmd, "DATA"):
				inData = true
				write("354 go ahead")
			case strings.HasPrefix(cmd, "QUIT"):
				write("221 bye")
				return
			default:
				write("250 OK")
			}
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port
}

func TestNewSMTPEmailSender_Validation(t *testing.T) {
	logger := testLogger()

	if _, err := NewSMTPEmailSender(nil, logger); !errors.Is(err, ErrEmailNotConfigured) {
		t.Errorf("nil config: error = %v, want ErrEmailNotConfigured", err)
	}
	cases := []struct {
		name string
		cfg  SMTPConfig
	}{
		{"missing host", SMTPConfig{Port: 25, From: "a@b.c"}},
		{"missing port", SMTPConfig{Host: "h", From: "a@b.c"}},
		{"missing from", SMTPConfig{Host: "h", Port: 25}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewSMTPEmailSender(&tc.cfg, logger); !errors.Is(err, ErrEmailNotConfigured) {
				t.Errorf("error = %v, want ErrEmailNotConfigured", err)
			}
		})
	}

	s, err := NewSMTPEmailSender(&SMTPConfig{Host: "h", Port: 25, From: "a@b.c"}, logger)
	if err != nil || s == nil {
		t.Fatalf("valid config: sender=%v err=%v", s, err)
	}
}

func TestSMTPEmailSender_SendSuccess(t *testing.T) {
	host, port := startFakeSMTP(t, false)
	s, err := NewSMTPEmailSender(&SMTPConfig{
		Host:     host,
		Port:     port,
		From:     "noreply@example.com",
		FromName: "NVNM Chain",
	}, testLogger())
	if err != nil {
		t.Fatalf("NewSMTPEmailSender: %v", err)
	}
	if err := s.Send(context.Background(), "user@example.com", "hello", "body text"); err != nil {
		t.Fatalf("Send: %v", err)
	}
}

func TestSMTPEmailSender_SendWithAuthFails(t *testing.T) {
	host, port := startFakeSMTP(t, true)
	s, err := NewSMTPEmailSender(&SMTPConfig{
		Host:     host,
		Port:     port,
		Username: "user",
		Password: "pass",
		From:     "noreply@example.com",
	}, testLogger())
	if err != nil {
		t.Fatalf("NewSMTPEmailSender: %v", err)
	}
	if err := s.Send(context.Background(), "user@example.com", "hello", "body"); err == nil {
		t.Fatal("Send should fail when the server rejects MAIL FROM")
	}
}

func TestSMTPEmailSender_EmptyRecipient(t *testing.T) {
	s, err := NewSMTPEmailSender(&SMTPConfig{Host: "h", Port: 25, From: "a@b.c"}, testLogger())
	if err != nil {
		t.Fatalf("NewSMTPEmailSender: %v", err)
	}
	if err := s.Send(context.Background(), "", "subject", "body"); !errors.Is(err, ErrEmailEmptyRecipient) {
		t.Errorf("error = %v, want ErrEmailEmptyRecipient", err)
	}
}

func TestSMTPEmailSender_HeaderInjectionRejected(t *testing.T) {
	s, err := NewSMTPEmailSender(&SMTPConfig{Host: "h", Port: 25, From: "a@b.c"}, testLogger())
	if err != nil {
		t.Fatalf("NewSMTPEmailSender: %v", err)
	}
	cases := []struct {
		name, to, subject string
	}{
		{"newline in recipient", "evil@example.com\nRCPT TO:<x>", "subject"},
		{"carriage return in subject", "user@example.com", "subject\r\nBcc: y"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := s.Send(context.Background(), tc.to, tc.subject, "body"); !errors.Is(err, ErrEmailHeaderInjection) {
				t.Errorf("error = %v, want ErrEmailHeaderInjection", err)
			}
		})
	}
}

func TestLogOnlyEmailSender_Send(t *testing.T) {
	s := NewLogOnlyEmailSender(testLogger())
	if err := s.Send(context.Background(), "user@example.com", "subject", "body"); err != nil {
		t.Errorf("LogOnlyEmailSender.Send should always return nil, got %v", err)
	}
}
