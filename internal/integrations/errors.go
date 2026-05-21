package integrations

import "errors"

var (
	// ErrWebhookNotSupported возвращается провайдером в RegisterWebhook,
	// если push-уведомления не поддерживаются и нужно использовать polling.
	ErrWebhookNotSupported = errors.New("webhook not supported by provider")

	// ErrInvalidSignature — подпись webhook не проверена.
	ErrInvalidSignature = errors.New("invalid webhook signature")

	// ErrTokenExpired — access_token истёк и refresh не помог
	// (или refresh_token тоже невалиден).
	ErrTokenExpired = errors.New("oauth token expired")

	// ErrProviderNotRegistered — нет такого провайдера в Registry.
	ErrProviderNotRegistered = errors.New("provider not registered")
)
