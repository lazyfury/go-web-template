package message

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
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
