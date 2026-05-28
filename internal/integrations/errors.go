package integrations

import "errors"

var (
	ErrWebhookNotSupported = errors.New("webhook not supported by provider")

	ErrInvalidSignature = errors.New("invalid webhook signature")

	ErrTokenExpired = errors.New("oauth token expired")

	ErrProviderNotRegistered = errors.New("provider not registered")
)
