// Package messaging handles message delivery across channels.
package messaging

import (
	"fmt"
	"log/slog"
	"sync/atomic"

	businessv1 "github.com/rkrimper1/jarvis/gen/business"
)

var msgCounter atomic.Int64

// Router delivers messages to the appropriate channel.
type Router struct {
	log *slog.Logger
}

// New creates a new Router.
func New(log *slog.Logger) *Router {
	return &Router{log: log}
}

// Send delivers a message and returns the message ID and per-recipient delivery status.
func (r *Router) Send(
	recipients []string,
	channel businessv1.MessageChannel,
	subject, body string,
	encrypt bool,
) (messageID string, delivered, failed []string) {
	msgCounter.Add(1)
	messageID = fmt.Sprintf("msg-%06d", msgCounter.Load())

	for _, recipient := range recipients {
		if err := r.deliver(recipient, channel, subject, body, encrypt); err != nil {
			r.log.Warn("message delivery failed",
				slog.String("recipient", recipient),
				slog.String("channel", channel.String()),
				slog.Any("err", err),
			)
			failed = append(failed, recipient)
		} else {
			delivered = append(delivered, recipient)
		}
	}
	return messageID, delivered, failed
}

func (r *Router) deliver(recipient string, channel businessv1.MessageChannel, subject, body string, encrypt bool) error {
	// In production each channel would call its transport (SMTP, Stark Comms API, hologram projector, etc.)
	// Here we log and simulate success for all non-empty recipients.
	if recipient == "" {
		return fmt.Errorf("empty recipient")
	}
	encLabel := ""
	if encrypt {
		encLabel = "[ENCRYPTED] "
	}
	r.log.Info("message delivered",
		slog.String("to", recipient),
		slog.String("channel", channel.String()),
		slog.String("subject", encLabel+subject),
	)
	return nil
}
