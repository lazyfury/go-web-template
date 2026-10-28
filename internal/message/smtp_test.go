package message

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The SMTP sender used to be the only untested delivery path: a fake server is
// enough to pin the parts that matter — the TLS policy, the credential guard
// and the mail exchange itself.

func TestSendSMTPRequiresAdvertisedSTARTTLS(t *testing.T) {
	srv := startFakeSMTP(t, fakeSMTPNoTLS)
	_, err := sendToFake(t, srv, &MailOptions{
		From:       "sender@example.com",
		RequireTLS: true,
	})
	if err == nil || !strings.Contains(err.Error(), "does not advertise STARTTLS") {
		t.Fatalf("err = %v, want an explicit STARTTLS refusal", err)
	}
}

func TestSendSMTPFailsWhenSTARTTLSHandshakeFails(t *testing.T) {
	srv := startFakeSMTP(t, fakeSMTPStartTLSFails)
	_, err := sendToFake(t, srv, &MailOptions{
		From:       "sender@example.com",
		RequireTLS: true,
	})
	if err == nil || !strings.Contains(err.Error(), "smtp starttls") {
		t.Fatalf("err = %v, want a starttls error", err)
	}
}

func TestSendSMTPPlaintextWhenTLSNotRequired(t *testing.T) {
	srv := startFakeSMTP(t, fakeSMTPNoTLS)
	if _, err := sendToFake(t, srv, &MailOptions{From: "sender@example.com"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	select {
	case data := <-srv.messages:
		if !strings.Contains(data, "Subject: Hello") || !strings.Contains(data, "<b>Hi</b>") {
			t.Errorf("delivered message = %q", data)
		}
	case <-time.After(time.Second):
		t.Fatal("fake SMTP server received no DATA")
	}
}

func TestSendSMTPRefusesPlaintextCredentials(t *testing.T) {
	srv := startFakeSMTP(t, fakeSMTPNoTLS)
	_, err := sendToFake(t, srv, &MailOptions{
		From:     "sender@example.com",
		Username: "user",
		Password: "secret",
	})
	if err == nil || !strings.Contains(err.Error(), "refusing to send SMTP credentials") {
		t.Fatalf("err = %v, want a plaintext-credential refusal", err)
	}
}

func TestMailSenderRejectsInvalidAddresses(t *testing.T) {
	sender := NewMailSender(&MailOptions{From: "not-an-address"})
	_, err := sender.Send(context.Background(), &Delivery{
		Channel: ChannelEmail, Target: "to@example.com", Payload: `{"body":"x"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid mail from address") {
		t.Fatalf("err = %v, want an invalid-from error", err)
	}

	sender = NewMailSender(&MailOptions{From: "sender@example.com"})
	_, err = sender.Send(context.Background(), &Delivery{
		Channel: ChannelEmail, Target: "not-an-address", Payload: `{"body":"x"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid mail recipient") {
		t.Fatalf("err = %v, want an invalid-recipient error", err)
	}
}

func sendToFake(t *testing.T, srv *fakeSMTPServer, opts *MailOptions) (string, error) {
	t.Helper()

	host, port := srv.hostPort(t)
	opts.Host = host
	opts.Port = port
	opts.Timeout = 2 * time.Second

	sender := NewMailSender(opts)
	return sender.Send(context.Background(), &Delivery{
		Channel: ChannelEmail,
		Target:  "to@example.com",
		Payload: `{"subject":"Hello","body":"<b>Hi</b>"}`,
	})
}

type fakeSMTPMode int

const (
	// fakeSMTPNoTLS behaves like a plaintext relay: EHLO advertises nothing.
	fakeSMTPNoTLS fakeSMTPMode = iota
	// fakeSMTPStartTLSFails advertises STARTTLS but answers 454 to it.
	fakeSMTPStartTLSFails
)

type fakeSMTPServer struct {
	addr     string
	messages chan string
}

// startFakeSMTP runs a minimal SMTP responder on a loopback port. It only
// implements what net/smtp needs for the tested paths (EHLO, MAIL, RCPT, DATA,
// QUIT, STARTTLS).
func startFakeSMTP(t *testing.T, mode fakeSMTPMode) *fakeSMTPServer {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &fakeSMTPServer{addr: listener.Addr().String(), messages: make(chan string, 4)}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			srv.serve(conn, mode)
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-done
	})
	return srv
}

func (s *fakeSMTPServer) hostPort(t *testing.T) (host string, port int) {
	t.Helper()
	host, portText, err := net.SplitHostPort(s.addr)
	if err != nil {
		t.Fatalf("split %q: %v", s.addr, err)
	}
	port, err = strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("port %q: %v", portText, err)
	}
	return host, port
}

func (s *fakeSMTPServer) serve(conn net.Conn, mode fakeSMTPMode) {
	defer func() { _ = conn.Close() }()
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	reply := func(line string) {
		_, _ = fmt.Fprintf(writer, "%s\r\n", line)
		_ = writer.Flush()
	}

	reply("220 fake ESMTP")
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		command := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(command, "EHLO"):
			if mode == fakeSMTPStartTLSFails {
				reply("250-fake")
				reply("250 STARTTLS")
				continue
			}
			reply("250 fake")
		case command == "STARTTLS":
			reply("454 4.7.0 TLS not available")
		case strings.HasPrefix(command, "HELO"), strings.HasPrefix(command, "MAIL FROM"),
			strings.HasPrefix(command, "RCPT TO"), strings.HasPrefix(command, "RSET"):
			reply("250 ok")
		case command == "DATA":
			reply("354 end with a line containing only a period")
			var body strings.Builder
			for {
				dataLine, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if dataLine == ".\r\n" {
					break
				}
				body.WriteString(dataLine)
			}
			select {
			case s.messages <- body.String():
			default:
			}
			reply("250 queued")
		case command == "QUIT":
			reply("221 bye")
			return
		default:
			reply("502 command not implemented")
		}
	}
}
