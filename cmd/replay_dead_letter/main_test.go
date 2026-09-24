package main

import (
	"testing"

	"github.com/CCDD2022/seckill-system/internal/model"
)

func TestValidateFormatRejectsPoisonAndUnknownRoutes(t *testing.T) {
	valid := model.DeadLetter{
		MessageID: "create:1:2", RoutingKey: "order.create",
		Body: []byte(`{"user_id":1,"product_id":2,"quantity":1,"total_price":10}`),
	}
	if err := validateFormat(valid); err != nil {
		t.Fatalf("valid archived order rejected: %v", err)
	}
	tests := []struct {
		name string
		item model.DeadLetter
	}{
		{"unsupported route", model.DeadLetter{MessageID: "x", RoutingKey: "other", Body: []byte(`{}`)}},
		{"missing message ID", model.DeadLetter{RoutingKey: "order.create", Body: valid.Body}},
		{"invalid JSON", model.DeadLetter{MessageID: "x", RoutingKey: "order.create", Body: []byte(`{`)}},
		{"invalid quantity", model.DeadLetter{MessageID: "x", RoutingKey: "order.create", Body: []byte(`{"user_id":1,"product_id":2,"quantity":0,"total_price":10}`)}},
		{"cancel event ID mismatch", model.DeadLetter{MessageID: "event-1", RoutingKey: "order.canceled", Body: []byte(`{"event_id":"event-2","order_id":3,"user_id":1,"product_id":2,"quantity":1}`)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateFormat(tc.item); err == nil {
				t.Fatal("unsafe dead letter accepted for replay")
			}
		})
	}
}
