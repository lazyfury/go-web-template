package message

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Sender delivers one external channel. Implementations must be safe for
// sequential use by the worker and must honor ctx.
type Sender interface {
	Channel() Channel
	Send(ctx context.Context, delivery *Delivery) (providerMessageID string, err error)
}

// MailOptions configures the SMTP sender.
type MailOptions struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	// RequireTLS refuses to send unless the server advertises STARTTLS.
	// Without it, a stripped STARTTLS (or a plaintext-only relay) sends the
	// message body in the clear, and a server that advertises nothing would be
	// silently accepted. Config validation forbids false in production.
	RequireTLS bool
	Timeout    time.Duration
}

// WebhookOptions configures the webhook sender.
type WebhookOptions struct {
	Timeout      time.Duration
	Secret       string
	AllowPrivate bool
	Client       *http.Client
}

// SMSOptions configures the SMS sender.
type SMSOptions struct {
	Provider   string
	AccountSID string
	AuthToken  string
	From       string
	Endpoint   string
	Timeout    time.Duration
	Client     *http.Client
}

// NewMailSender creates an SMTP sender.
func NewMailSender(opts *MailOptions) Sender {
	if opts == nil {
		opts = &MailOptions{}
	}
	if opts.Port == 0 {
		opts.Port = 587
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	return &mailSender{opts: *opts, send: sendSMTP}
}

// NewWebhookSender creates a webhook sender.
//
// The default client enforces the SSRF policy on every dialed address rather
// than only on the configured URL, so redirects and DNS rebinding cannot reach
// a private address either. A caller-supplied Client owns its own policy.
func NewWebhookSender(opts WebhookOptions) Sender {
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	base := opts.Client
	if base == nil {
		base = newWebhookClient(opts.Timeout, opts.AllowPrivate, isBlockedIP)
	}
	// Trace outbound webhook calls as children of the delivery span. Copy the
	// client so a caller-supplied one is not mutated by instrumentation.
	client := *base
	if client.Transport == nil {
		client.Transport = http.DefaultTransport
	}
	client.Transport = otelhttp.NewTransport(client.Transport)
	return &webhookSender{opts: opts, client: &client}
}

// webhookRedirectLimit caps how many redirect hops one webhook may follow.
const webhookRedirectLimit = 5

func newWebhookClient(timeout time.Duration, allowPrivate bool, blocked func(net.IP) bool) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil // a proxy would hide the real target from the dial guard
	transport.DialContext = (&net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
		Control:   webhookDialControl(allowPrivate, blocked),
	}).DialContext
	transport.TLSHandshakeTimeout = timeout
	transport.ResponseHeaderTimeout = timeout
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= webhookRedirectLimit {
				return fmt.Errorf("webhook stopped after %d redirects", webhookRedirectLimit)
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("webhook redirect to unsupported scheme %q", req.URL.Scheme)
			}
			return nil
		},
	}
}

// webhookDialControl rejects non-public peer addresses at connect time. The
// check runs on the address actually dialed, so it also covers redirect hosts
// and names that resolve to a private address after the pre-flight lookup.
func webhookDialControl(allowPrivate bool, blocked func(net.IP) bool) func(string, string, syscall.RawConn) error {
	if allowPrivate {
		return nil
	}
	return func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return fmt.Errorf("invalid webhook dial address")
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return fmt.Errorf("webhook target resolves to an invalid address")
		}
		if blocked(ip) {
			return fmt.Errorf("webhook target resolves to a private address")
		}
		return nil
	}
}

// NewTwilioSender creates a Twilio SMS sender.
func NewTwilioSender(opts *SMSOptions) (Sender, error) {
	if opts == nil {
		return nil, fmt.Errorf("sms options are required")
	}
	options := *opts
	provider := strings.ToLower(strings.TrimSpace(options.Provider))
	if provider != "twilio" {
		return nil, fmt.Errorf("unsupported sms provider %q", options.Provider)
	}
	if options.AccountSID == "" || options.AuthToken == "" || options.From == "" {
		return nil, fmt.Errorf("twilio sms requires account_sid, auth_token and from")
	}
	if options.Endpoint == "" {
		options.Endpoint = fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", options.AccountSID)
	}
	parsed, err := url.Parse(options.Endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("invalid sms endpoint")
	}
	if options.Timeout <= 0 {
		options.Timeout = 30 * time.Second
	}
	client := options.Client
	if client == nil {
		client = &http.Client{Timeout: options.Timeout}
	}
	return &smsSender{opts: options, client: client}, nil
}

type mailSender struct {
	opts MailOptions
	send func(ctx context.Context, opts *MailOptions, to string, msg []byte) error
}

func (s *mailSender) Channel() Channel { return ChannelEmail }

func (s *mailSender) Send(ctx context.Context, delivery *Delivery) (string, error) {
	payload := decodePayload(delivery.Payload)
	if strings.TrimSpace(payload.Body) == "" {
		payload.Body = delivery.Payload
	}
	message, err := composeMail(s.opts.From, delivery.Target, payload.Subject, payload.Body)
	if err != nil {
		return "", err
	}
	if err := s.send(ctx, &s.opts, delivery.Target, message); err != nil {
		return "", fmt.Errorf("smtp send: %w", err)
	}
	return "", nil
}

func composeMail(fromRaw, toRaw, subject, body string) ([]byte, error) {
	from, err := mail.ParseAddress(fromRaw)
	if err != nil {
		return nil, fmt.Errorf("invalid mail from address: %w", err)
	}
	to, err := mail.ParseAddress(toRaw)
	if err != nil {
		return nil, fmt.Errorf("invalid mail recipient: %w", err)
	}
	encodedSubject := mime.QEncoding.Encode("utf-8", subject)
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from.String())
	fmt.Fprintf(&b, "To: %s\r\n", to.String())
	fmt.Fprintf(&b, "Subject: %s\r\n", encodedSubject)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return []byte(b.String()), nil
}

func sendSMTP(ctx context.Context, opts *MailOptions, to string, message []byte) error {
	from, err := mail.ParseAddress(opts.From)
	if err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	recipient, err := mail.ParseAddress(to)
	if err != nil {
		return fmt.Errorf("invalid recipient: %w", err)
	}
	address := net.JoinHostPort(opts.Host, strconv.Itoa(opts.Port))
	dialer := &net.Dialer{Timeout: opts.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("dial %s: %w", opts.Host, err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(opts.Timeout))

	client, err := smtp.NewClient(conn, opts.Host)
	if err != nil {
		return fmt.Errorf("smtp handshake: %w", err)
	}
	defer func() { _ = client.Close() }()

	advertisesTLS, _ := client.Extension("STARTTLS")
	if opts.RequireTLS && !advertisesTLS {
		return fmt.Errorf("smtp server %s does not advertise STARTTLS but mail.require_tls is true", opts.Host)
	}
	if advertisesTLS {
		if err := client.StartTLS(&tls.Config{ServerName: opts.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}
	if opts.Username != "" {
		// net/smtp's PlainAuth already refuses plaintext non-localhost servers;
		// keep the explicit check so a future auth mechanism cannot regress it.
		if _, tlsConn := client.TLSConnectionState(); !tlsConn {
			return fmt.Errorf("refusing to send SMTP credentials to %s over a plaintext connection: enable STARTTLS or clear mail.username", opts.Host)
		}
		if err := client.Auth(smtp.PlainAuth("", opts.Username, opts.Password, opts.Host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := client.Mail(from.Address); err != nil {
		return fmt.Errorf("smtp mail: %w", err)
	}
	if err := client.Rcpt(recipient.Address); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := writer.Write(message); err != nil {
		_ = writer.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("smtp close data: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("smtp quit: %w", err)
	}
	return nil
}

type webhookSender struct {
	opts   WebhookOptions
	client *http.Client
}

func (s *webhookSender) Channel() Channel { return ChannelWebhook }

func (s *webhookSender) Send(ctx context.Context, delivery *Delivery) (string, error) {
	if err := checkWebhookTarget(ctx, delivery.Target, s.opts.AllowPrivate); err != nil {
		return "", err
	}
	body := []byte(delivery.Payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, delivery.Target, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Message-ID", strconv.FormatUint(delivery.MessageID, 10))
	req.Header.Set("X-Delivery-ID", strconv.FormatUint(delivery.ID, 10))
	if s.opts.Secret != "" {
		mac := hmac.New(sha256.New, []byte(s.opts.Secret))
		_, _ = mac.Write(body)
		req.Header.Set("X-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("webhook request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		excerpt := strings.TrimSpace(string(responseBody))
		if len(excerpt) > 200 {
			excerpt = excerpt[:200]
		}
		return "", fmt.Errorf("webhook returned %d: %s", resp.StatusCode, excerpt)
	}
	return resp.Header.Get("X-Request-ID"), nil
}

func checkWebhookTarget(ctx context.Context, raw string, allowPrivate bool) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("invalid webhook url")
	}
	if allowPrivate {
		return nil
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, parsed.Hostname())
	if err != nil {
		return fmt.Errorf("resolve webhook host: %w", err)
	}
	for _, address := range addresses {
		if isBlockedIP(address.IP) {
			return fmt.Errorf("webhook target resolves to a private address")
		}
	}
	return nil
}

func isBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

type smsSender struct {
	opts   SMSOptions
	client *http.Client
}

func (s *smsSender) Channel() Channel { return ChannelSMS }

func (s *smsSender) Send(ctx context.Context, delivery *Delivery) (string, error) {
	payload := decodePayload(delivery.Payload)
	form := url.Values{}
	form.Set("To", delivery.Target)
	form.Set("From", s.opts.From)
	form.Set("Body", payload.Body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.opts.Endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("build sms request: %w", err)
	}
	req.SetBasicAuth(s.opts.AccountSID, s.opts.AuthToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("sms request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("sms provider returned %d", resp.StatusCode)
	}
	var result struct {
		SID string `json:"sid"`
	}
	_ = json.Unmarshal(responseBody, &result)
	return result.SID, nil
}

func decodePayload(raw string) deliveryPayload {
	var payload deliveryPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return deliveryPayload{Body: raw}
	}
	return payload
}
