package notify

import "context"

type Message struct {
	UserID     string
	UserName   string
	UserEmail  string
	TelegramID string
	Title      string
	Body       string
	Link       string
	Kind       string
}

type Transport interface {
	Name() string
	Send(ctx context.Context, msg Message) error
}

type EnabledChecker interface {
	Enabled() bool
}
