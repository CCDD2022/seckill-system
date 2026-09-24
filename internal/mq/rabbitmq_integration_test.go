package mq

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/CCDD2022/seckill-system/config"
	"github.com/streadway/amqp"
)

func TestPublishConfirmedWithIDAgainstBroker(t *testing.T) {
	if os.Getenv("MQ_INTEGRATION") != "1" {
		t.Skip("set MQ_INTEGRATION=1 against a disposable RabbitMQ broker")
	}
	password, err := os.ReadFile(os.Getenv("MQ_TEST_PASSWORD_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.MQConfig{Host: "rabbitmq", Port: 5672, User: "seckill", Password: strings.TrimSpace(string(password)), ChannelPoolSize: 1}
	conn, err := amqp.Dial(fmt.Sprintf("amqp://%s:%s@%s:%d/", cfg.User, cfg.Password, cfg.Host, cfg.Port))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer ch.Close()
	name := fmt.Sprintf("seckill.confirm.test.%d", time.Now().UnixNano())
	if err := ch.ExchangeDeclare(name, "topic", true, false, false, false, nil); err != nil {
		t.Fatal(err)
	}
	defer ch.ExchangeDelete(name, false, false)
	if _, err := ch.QueueDeclare(name, true, false, false, false, nil); err != nil {
		t.Fatal(err)
	}
	defer ch.QueueDelete(name, false, false, false)
	if err := ch.QueueBind(name, "routed", name, false, nil); err != nil {
		t.Fatal(err)
	}
	producer, err := Init(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := producer.PublishConfirmedWithID(ctx, name, "routed", []byte(`{"ok":true}`), "confirmed-1"); err != nil {
		t.Fatalf("routed publish not confirmed: %v", err)
	}
	message, ok, err := ch.Get(name, true)
	if err != nil || !ok || message.MessageId != "confirmed-1" {
		t.Fatalf("confirmed message missing from queue: found=%v id=%q err=%v", ok, message.MessageId, err)
	}
	if err := producer.PublishConfirmedWithID(ctx, name, "no-binding", []byte(`{"ok":false}`), "returned-1"); err == nil || !strings.Contains(err.Error(), "unroutable") {
		t.Fatalf("mandatory unroutable publish was not rejected: %v", err)
	}
}
