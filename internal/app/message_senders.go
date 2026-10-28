package app

import (
	"fmt"

	"github.com/example/go-web-template/internal/message"
	"github.com/example/go-web-template/internal/platform/config"
)

// buildMessageSenders maps configuration to message.Sender implementations.
// A channel with no configuration simply has no sender; the worker marks such
// deliveries dead with "sender not configured".
func buildMessageSenders(cfg *config.Config) (map[message.Channel]message.Sender, error) {
	senders := make(map[message.Channel]message.Sender, 3)

	if len(cfg.Message.WebhookURLs) > 0 {
		senders[message.ChannelWebhook] = message.NewWebhookSender(message.WebhookOptions{
			Timeout:      cfg.Message.DeliveryTimeout,
			Secret:       cfg.Message.WebhookSecret,
			AllowPrivate: cfg.Message.WebhookAllowPrivate,
		})
	}
	if cfg.Message.Mail.Host != "" {
		senders[message.ChannelEmail] = message.NewMailSender(&message.MailOptions{
			Host:       cfg.Message.Mail.Host,
			Port:       cfg.Message.Mail.Port,
			Username:   cfg.Message.Mail.Username,
			Password:   cfg.Message.Mail.Password,
			From:       cfg.Message.Mail.From,
			RequireTLS: cfg.Message.Mail.RequireTLS,
			Timeout:    cfg.Message.DeliveryTimeout,
		})
	}
	if cfg.Message.SMS.Provider != "" {
		sender, err := message.NewTwilioSender(&message.SMSOptions{
			Provider:   cfg.Message.SMS.Provider,
			AccountSID: cfg.Message.SMS.AccountSID,
			AuthToken:  cfg.Message.SMS.AuthToken,
			From:       cfg.Message.SMS.From,
			Endpoint:   cfg.Message.SMS.Endpoint,
			Timeout:    cfg.Message.DeliveryTimeout,
		})
		if err != nil {
			return nil, fmt.Errorf("configure sms sender: %w", err)
		}
		senders[message.ChannelSMS] = sender
	}
	return senders, nil
}
