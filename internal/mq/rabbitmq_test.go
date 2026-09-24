package mq

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/streadway/amqp"
)

func TestAwaitPublishConfirm(t *testing.T) {
	tests := []struct {
		name        string
		confirm     *amqp.Confirmation
		returned    *amqp.Return
		wantErr     string
		uncertain   bool
		wantHealthy bool
	}{
		{name: "routed and confirmed", confirm: &amqp.Confirmation{Ack: true}, wantHealthy: true},
		{name: "unroutable but broker acked", returned: &amqp.Return{ReplyCode: 312, ReplyText: "NO_ROUTE"}, confirm: &amqp.Confirmation{Ack: true}, wantErr: "unroutable", wantHealthy: true},
		{name: "broker nack", confirm: &amqp.Confirmation{Ack: false}, wantErr: "nacked", wantHealthy: true},
		{name: "confirmation timeout", wantErr: "timeout", uncertain: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			confirms := make(chan amqp.Confirmation, 1)
			returns := make(chan amqp.Return, 1)
			if tc.returned != nil {
				returns <- *tc.returned
			}
			if tc.confirm != nil {
				confirms <- *tc.confirm
			}
			timeout := 50 * time.Millisecond
			if tc.name == "confirmation timeout" {
				timeout = 5 * time.Millisecond
			}
			healthy, err := awaitPublishConfirm(context.Background(), confirms, returns, make(chan struct{}), "test-event", timeout)
			if healthy != tc.wantHealthy {
				t.Fatalf("channel healthy = %v, want %v", healthy, tc.wantHealthy)
			}
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected publish error: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("publish error = %v, want %q", err, tc.wantErr)
			}
			if errors.Is(err, ErrPublishUncertain) != tc.uncertain {
				t.Fatalf("uncertain = %v, want %v", errors.Is(err, ErrPublishUncertain), tc.uncertain)
			}
		})
	}
}

func TestAwaitPublishConfirmCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	healthy, err := awaitPublishConfirm(ctx, make(chan amqp.Confirmation), make(chan amqp.Return), make(chan struct{}), "test-event", time.Second)
	if healthy || !errors.Is(err, ErrPublishUncertain) {
		t.Fatalf("canceled publish = (healthy %v, error %v)", healthy, err)
	}
}
