package main

import (
	"bytes"
	"testing"

	"github.com/streadway/amqp"
)

func TestArchiveRecordKeepsOriginalDelivery(t *testing.T) {
	delivery := amqp.Delivery{
		MessageId: "create:11:17",
		Exchange:  "seckill.dlx", RoutingKey: "order.create",
		Body: []byte{0, '\n', 255, 'x'},
	}
	first := archiveRecord(delivery)
	second := archiveRecord(delivery)
	if first.DeliveryKey == "" || first.DeliveryKey != second.DeliveryKey {
		t.Fatal("redelivery did not produce a stable archive key")
	}
	if !bytes.Equal(first.Body, delivery.Body) || first.MessageID != delivery.MessageId || first.RoutingKey != delivery.RoutingKey {
		t.Fatal("archive record lost delivery metadata or binary body")
	}
	changed := delivery
	changed.Body = []byte("different payload")
	if archiveRecord(changed).DeliveryKey == first.DeliveryKey {
		t.Fatal("distinct payload collapsed into one archive record")
	}
}
