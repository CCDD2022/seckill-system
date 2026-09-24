package mq

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/CCDD2022/seckill-system/config"
	"github.com/CCDD2022/seckill-system/pkg/logger"
	"github.com/streadway/amqp"
)

// ErrPublishUncertain means that the connection failed or confirmation timed out
// after Publish was attempted. The broker may have accepted the message. Callers
// must not compensate a reservation merely because this error was returned.
var ErrPublishUncertain = errors.New("rabbitmq publish outcome uncertain")

var ErrPoolClosed = errors.New("rabbitmq producer pool closed")

const publishConfirmTimeout = 10 * time.Second

type ChannelWrapper struct {
	ch       *amqp.Channel
	confirms <-chan amqp.Confirmation
	returns  <-chan amqp.Return
}

// Pool lends each producer channel exclusively until its message has been
// confirmed. This keeps a confirmation and a returned message associated with
// the publish that produced them.
type Pool struct {
	conn     *amqp.Connection
	channels chan *ChannelWrapper
	done     chan struct{}
	mu       sync.Mutex
	closed   bool
}

func Init(cfg *config.MQConfig) (*Pool, error) {
	url := fmt.Sprintf("amqp://%s:%s@%s:%d/", cfg.User, cfg.Password, cfg.Host, cfg.Port)
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("dial rabbitmq failed: %w", err)
	}
	size := cfg.ChannelPoolSize
	if size <= 0 {
		size = 24
	}
	p := &Pool{conn: conn, channels: make(chan *ChannelWrapper, size), done: make(chan struct{})}
	for i := 0; i < size; i++ {
		cw, err := p.createChannelWrapper()
		if err != nil {
			p.Close()
			return nil, fmt.Errorf("open producer channel failed: %w", err)
		}
		p.channels <- cw
	}
	logger.Info("MQ producer channel pool initialized", "size", size)
	return p, nil
}

func (p *Pool) createChannelWrapper() (*ChannelWrapper, error) {
	ch, err := p.conn.Channel()
	if err != nil {
		return nil, err
	}
	if err := ch.Confirm(false); err != nil {
		_ = ch.Close()
		return nil, fmt.Errorf("enable publisher confirms failed: %w", err)
	}
	return &ChannelWrapper{
		ch:       ch,
		confirms: ch.NotifyPublish(make(chan amqp.Confirmation, 1)),
		returns:  ch.NotifyReturn(make(chan amqp.Return, 1)),
	}, nil
}

func (p *Pool) acquire(ctx context.Context) (*ChannelWrapper, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.done:
		return nil, ErrPoolClosed
	case cw := <-p.channels:
		return cw, nil
	}
}

func (p *Pool) release(cw *ChannelWrapper, healthy bool) {
	if !healthy {
		_ = cw.ch.Close()
		var err error
		cw, err = p.createChannelWrapper()
		if err != nil {
			logger.Error("replace producer channel failed", "err", err)
			p.Close()
			return
		}
	}
	select {
	case <-p.done:
		_ = cw.ch.Close()
	case p.channels <- cw:
	}
}

func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	close(p.done)
	_ = p.conn.Close()
}

// EnsureBaseTopology declares the exchange before any publish. The queues are
// declared and bound by their consumers.
func (p *Pool) EnsureBaseTopology() error {
	ch, err := p.conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	if err := ch.ExchangeDeclare("seckill.exchange", "topic", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare exchange failed: %w", err)
	}
	return nil
}

// EnsureOrderDeadLetters provisions the durable target before either order
// consumer starts. Both order queues route failed deliveries to this queue.
func EnsureOrderDeadLetters(cfg *config.MQConfig) error {
	url := fmt.Sprintf("amqp://%s:%s@%s:%d/", cfg.User, cfg.Password, cfg.Host, cfg.Port)
	conn, err := amqp.Dial(url)
	if err != nil {
		return fmt.Errorf("dial rabbitmq failed: %w", err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	if err := ch.ExchangeDeclare("seckill.dlx", "topic", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dead letter exchange failed: %w", err)
	}
	if _, err := ch.QueueDeclare("order.create.dlq", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dead letter queue failed: %w", err)
	}
	if err := ch.QueueBind("order.create.dlq", "#", "seckill.dlx", false, nil); err != nil {
		return fmt.Errorf("bind dead letter queue failed: %w", err)
	}
	return nil
}

// PublishAsyncWithID retains the original API name for callers, but now waits
// for broker confirmation and rejects unroutable messages. A successful return
// means RabbitMQ accepted the persistent message for a bound queue, not that
// the consumer has committed it to the database.
func (p *Pool) PublishAsyncWithID(exchange, key string, body []byte, messageID string) error {
	return p.PublishConfirmedWithID(context.Background(), exchange, key, body, messageID)
}

// PublishConfirmedWithID waits for broker confirmation or context cancellation.
func (p *Pool) PublishConfirmedWithID(ctx context.Context, exchange, key string, body []byte, messageID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cw, err := p.acquire(ctx)
	if err != nil {
		return err
	}
	healthy := true
	defer func() { p.release(cw, healthy) }()
	if err := ctx.Err(); err != nil {
		return err
	}

	err = cw.ch.Publish(exchange, key, true, false, amqp.Publishing{
		ContentType:  "application/json",
		Body:         body,
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now(),
		MessageId:    messageID,
	})
	if err != nil {
		healthy = false
		return fmt.Errorf("%w: publish failed: %v", ErrPublishUncertain, err)
	}

	healthy, err = awaitPublishConfirm(ctx, cw.confirms, cw.returns, p.done, messageID, publishConfirmTimeout)
	return err
}

// awaitPublishConfirm is separate from network I/O so the ordering of return,
// ACK, NACK, timeout and cancellation can be tested deterministically.
func awaitPublishConfirm(ctx context.Context, confirms <-chan amqp.Confirmation, returns <-chan amqp.Return, done <-chan struct{}, messageID string, timeout time.Duration) (bool, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var returned *amqp.Return
	for {
		select {
		case <-ctx.Done():
			return false, fmt.Errorf("%w: %v", ErrPublishUncertain, ctx.Err())
		case ret, ok := <-returns:
			if !ok {
				return false, fmt.Errorf("%w: return channel closed", ErrPublishUncertain)
			}
			returned = &ret
		case confirm, ok := <-confirms:
			if !ok {
				return false, fmt.Errorf("%w: confirmation channel closed", ErrPublishUncertain)
			}
			if !confirm.Ack {
				return true, fmt.Errorf("rabbitmq nacked message %q", messageID)
			}
			// RabbitMQ sends basic.return before basic.ack for mandatory
			// unroutable messages. Drain in case both channels became ready.
			select {
			case ret, ok := <-returns:
				if ok {
					returned = &ret
				}
			default:
			}
			if returned != nil {
				return true, fmt.Errorf("rabbitmq returned unroutable message %q: %d %s", messageID, returned.ReplyCode, returned.ReplyText)
			}
			return true, nil
		case <-timer.C:
			return false, fmt.Errorf("%w: confirmation timeout for %q", ErrPublishUncertain, messageID)
		case <-done:
			return false, fmt.Errorf("%w: producer pool closed", ErrPublishUncertain)
		}
	}
}

func NewConsumerChannel(cfg *config.MQConfig, queue, bindKey, exchange string, durable bool, prefetch int, args amqp.Table) (*amqp.Connection, *amqp.Channel, <-chan amqp.Delivery, error) {
	url := fmt.Sprintf("amqp://%s:%s@%s:%d/", cfg.User, cfg.Password, cfg.Host, cfg.Port)
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("dial rabbitmq failed: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, nil, nil, fmt.Errorf("open channel failed: %w", err)
	}
	if exchange != "" {
		if err := ch.ExchangeDeclare(exchange, "topic", true, false, false, false, nil); err != nil {
			ch.Close()
			conn.Close()
			return nil, nil, nil, fmt.Errorf("declare exchange failed: %w", err)
		}
	}
	if _, err := ch.QueueDeclare(queue, durable, false, false, false, args); err != nil {
		ch.Close()
		conn.Close()
		return nil, nil, nil, fmt.Errorf("declare queue failed: %w", err)
	}
	if bindKey != "" && exchange != "" {
		if err := ch.QueueBind(queue, bindKey, exchange, false, nil); err != nil {
			ch.Close()
			conn.Close()
			return nil, nil, nil, fmt.Errorf("bind queue failed: %w", err)
		}
	}
	if prefetch > 0 {
		if err := ch.Qos(prefetch, 0, false); err != nil {
			ch.Close()
			conn.Close()
			return nil, nil, nil, fmt.Errorf("set qos failed: %w", err)
		}
	}
	msgs, err := ch.Consume(queue, "", false, false, false, false, nil)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, nil, nil, fmt.Errorf("consume failed: %w", err)
	}
	return conn, ch, msgs, nil
}

func CloseConsumer(conn *amqp.Connection, ch *amqp.Channel) {
	if ch != nil {
		_ = ch.Close()
	}
	if conn != nil {
		_ = conn.Close()
	}
}
