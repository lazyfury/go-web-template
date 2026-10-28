package message

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWebhookSenderSignsAndPosts(t *testing.T) {
	const secret = "test-secret"
	var gotSignature, gotBody, gotDeliveryID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSignature = r.Header.Get("X-Signature")
		gotDeliveryID = r.Header.Get("X-Delivery-ID")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("X-Request-ID", "req-1")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	sender := NewWebhookSender(WebhookOptions{Secret: secret, AllowPrivate: true})
	providerID, err := sender.Send(context.Background(), &Delivery{
		ID: 7, MessageID: 9, Channel: ChannelWebhook, Target: server.URL, Payload: `{"subject":"hi","body":"there"}`,
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if providerID != "req-1" {
		t.Errorf("provider id = %q", providerID)
	}
	if gotDeliveryID != "7" || gotBody != `{"subject":"hi","body":"there"}` {
		t.Errorf("request headers/body = delivery %q body %q", gotDeliveryID, gotBody)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(gotBody))
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if gotSignature != want {
		t.Errorf("signature = %q, want %q", gotSignature, want)
	}
}

func TestWebhookSenderRejectsPrivateTarget(t *testing.T) {
	sender := NewWebhookSender(WebhookOptions{})
	_, err := sender.Send(context.Background(), &Delivery{
		Channel: ChannelWebhook, Target: "http://127.0.0.1:1/hook", Payload: `{}`,
	})
	if err == nil {
		t.Fatal("private target should be blocked")
	}
}

// TestIsBlockedIP pins the guard's address classification, including IPv4-in-IPv6
// forms that would otherwise slip past a naive string prefix check.
func TestIsBlockedIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "::1", "::ffff:127.0.0.1",
		"10.0.0.5", "172.16.0.1", "192.168.1.1", "::ffff:10.0.0.5",
		"169.254.169.254", "fe80::1", "0.0.0.0", "224.0.0.1", "ff02::1",
	}
	for _, raw := range blocked {
		if !isBlockedIP(net.ParseIP(raw)) {
			t.Errorf("isBlockedIP(%s) = false, want true", raw)
		}
	}
	allowed := []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"}
	for _, raw := range allowed {
		if isBlockedIP(net.ParseIP(raw)) {
			t.Errorf("isBlockedIP(%s) = true, want false", raw)
		}
	}
}

// TestWebhookDialControlRejectsNonPublicAddresses covers the dial-time guard that
// makes redirects and DNS rebinding ineffective.
func TestWebhookDialControlRejectsNonPublicAddresses(t *testing.T) {
	control := webhookDialControl(false, isBlockedIP)
	for _, address := range []string{"127.0.0.1:80", "[::1]:80", "169.254.169.254:80", "192.168.1.1:443"} {
		if err := control("tcp", address, nil); err == nil {
			t.Errorf("control(%s) allowed a private address", address)
		}
	}
	for _, address := range []string{"8.8.8.8:443", "[2606:4700:4700::1111]:443"} {
		if err := control("tcp", address, nil); err != nil {
			t.Errorf("control(%s) = %v, want nil", address, err)
		}
	}
	if err := control("tcp", "not-an-address", nil); err == nil {
		t.Error("control accepted an address without a port")
	}
}

// deliveryRequest builds the same kind of request the sender sends, so tests do
// not fall back to the context-less Client.Get/Post helpers.
func deliveryRequest(t *testing.T, target string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, target, strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return req
}

// TestWebhookClientBlocksRedirectToPrivateTarget pins that a public endpoint
// cannot bounce the worker at a private address: the guard runs on the redirect
// hop's dial, not just on the configured target.
func TestWebhookClientBlocksRedirectToPrivateTarget(t *testing.T) {
	metadata := net.IPv4(169, 254, 169, 254)
	client := newWebhookClient(5*time.Second, false, func(ip net.IP) bool { return ip.Equal(metadata) })
	defer client.CloseIdleConnections()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer server.Close()

	resp, err := client.Do(deliveryRequest(t, server.URL))
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("redirect to a private address should be blocked")
	}
	if !strings.Contains(err.Error(), "private address") {
		t.Errorf("error = %v, want a private-address rejection", err)
	}
}

func TestWebhookClientCapsRedirects(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.Redirect(w, r, "/loop", http.StatusFound)
	}))
	defer server.Close()

	client := newWebhookClient(5*time.Second, true, isBlockedIP)
	defer client.CloseIdleConnections()
	resp, err := client.Do(deliveryRequest(t, server.URL))
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("a redirect loop should be capped")
	}
	if got := atomic.LoadInt32(&hits); got != webhookRedirectLimit {
		t.Errorf("server saw %d requests, want %d", got, webhookRedirectLimit)
	}
}

func TestTwilioSenderPostsFormAndReturnsSID(t *testing.T) {
	var gotForm url.Values
	var gotUser, gotPassword string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPassword, _ = r.BasicAuth()
		_ = r.ParseForm()
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"sid": "SM123"})
	}))
	defer server.Close()

	sender, err := NewTwilioSender(&SMSOptions{
		Provider: "twilio", AccountSID: "AC123", AuthToken: "token", From: "+100",
		Endpoint: server.URL,
	})
	if err != nil {
		t.Fatalf("NewTwilioSender: %v", err)
	}
	sid, err := sender.Send(context.Background(), &Delivery{
		Channel: ChannelSMS, Target: "+200", Payload: `{"body":"hello"}`,
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if sid != "SM123" {
		t.Errorf("sid = %q", sid)
	}
	if gotUser != "AC123" || gotPassword != "token" {
		t.Errorf("basic auth = %q/%q", gotUser, gotPassword)
	}
	if gotForm.Get("To") != "+200" || gotForm.Get("From") != "+100" || gotForm.Get("Body") != "hello" {
		t.Errorf("form = %v", gotForm)
	}
}

func TestMailSenderComposesMessage(t *testing.T) {
	sender := NewMailSender(&MailOptions{From: "Sender <from@example.com>"}).(*mailSender)
	var gotTo string
	var gotMessage []byte
	sender.send = func(_ context.Context, _ *MailOptions, to string, message []byte) error {
		gotTo = to
		gotMessage = append([]byte(nil), message...)
		return nil
	}

	if _, err := sender.Send(context.Background(), &Delivery{
		Channel: ChannelEmail, Target: "to@example.com", Payload: `{"subject":"Hello","body":"<b>Hi</b>"}`,
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotTo != "to@example.com" {
		t.Errorf("to = %q", gotTo)
	}
	text := string(gotMessage)
	for _, want := range []string{"From: \"Sender\" <from@example.com>", "To: <to@example.com>", "Content-Type: text/html", "<b>Hi</b>"} {
		if !strings.Contains(text, want) {
			t.Errorf("message missing %q:\n%s", want, text)
		}
	}
}

// TestSendersReportTheirChannel keeps the worker's sender map honest: a mismatched
// Channel() would route deliveries to the wrong adapter and only fail in prod.
func TestSendersReportTheirChannel(t *testing.T) {
	if got := NewMailSender(nil).Channel(); got != ChannelEmail {
		t.Errorf("mail sender channel = %q, want %q", got, ChannelEmail)
	}
	if got := NewWebhookSender(WebhookOptions{}).Channel(); got != ChannelWebhook {
		t.Errorf("webhook sender channel = %q, want %q", got, ChannelWebhook)
	}
	sms, err := NewTwilioSender(&SMSOptions{Provider: "twilio", AccountSID: "AC1", AuthToken: "token", From: "+100"})
	if err != nil {
		t.Fatalf("NewTwilioSender: %v", err)
	}
	if got := sms.Channel(); got != ChannelSMS {
		t.Errorf("sms sender channel = %q, want %q", got, ChannelSMS)
	}
}
