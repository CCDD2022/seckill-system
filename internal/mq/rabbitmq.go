package mq

// 高并发生产者专用 RabbitMQ 封装：
// - 根据配置初始化连接与生产者通道池
// - 使用异步 Confirm：发布后不阻塞等待 ACK，后台协程统一处理
// - 消费者不使用池，每个消费者独立创建 Channel

import (
	"fmt"
	"sync"
	"time"

	"github.com/CCDD2022/seckill-system/config"
	"github.com/CCDD2022/seckill-system/pkg/logger"
	"github.com/streadway/amqp"
)

type channelWrapper struct {
	ch *amqp.Channel
	// 只读通道  接收发布确认结果(来自rabbitMQ服务器)
	confirms <-chan amqp.Confirmation
}

// Pool 维护一个连接与一组生产者通道（带异步确认处理）。
type Pool struct {
	conn     *amqp.Connection
	channels chan *channelWrapper
	size     int
	mu       sync.Mutex // 防止Close被并发调用
	closed   bool
}

// Init 创建连接与生产者通道池，所有通道开启 Confirm 模式并启动后台确认处理。
func Init(cfg *config.MQConfig) (*Pool, error) {
	// 连接rabbitMQ服务器
	url := fmt.Sprintf("amqp://%s:%s@%s:%d/", cfg.User, cfg.Password, cfg.Host, cfg.Port)
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("dial rabbitmq failed: %w", err)
	}
	size := cfg.ChannelPoolSize
	if size <= 0 {
		size = 24
	}

	// 创建通道池
	p := &Pool{conn: conn, channels: make(chan *channelWrapper, size), size: size}
	for i := 0; i < size; i++ {
		cw, err := p.createChannelWrapper()
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("open channel failed: %w", err)
		}
		p.channels <- cw
	}
	logger.Info("MQ producer channel pool initialized", "size", size)
	return p, nil
}

// createChannelWrapper 创建一个带异步确认处理的生产者通道包装
func (p *Pool) createChannelWrapper() (*channelWrapper, error) {
	ch, err := p.conn.Channel()
	if err != nil {
		return nil, err
	}
	if err := ch.Confirm(false); err != nil {
		_ = ch.Close()
		return nil, fmt.Errorf("enable confirm failed: %w", err)
	}

	// 创建确认监听器  返回带缓冲的确认通道
	// 可积压1024个确认结果，避免阻塞发布协程
	conf := ch.NotifyPublish(make(chan amqp.Confirmation, 1024))
	// 后台异步处理确认结果：仅记录 Nack
	go func(c <-chan amqp.Confirmation) {
		for cf := range c {
			// Ack=true表示消息已经成功送到rabbitMQ服务器
			if !cf.Ack {
				logger.Warn("publish not acked", "delivery_tag", cf.DeliveryTag)
			}
		}
	}(conf)
	return &channelWrapper{ch: ch, confirms: conf}, nil
}

// Acquire 获取一个可用生产者ChannelWrapper
func (p *Pool) Acquire() *channelWrapper {
	return <-p.channels
}

// Release 归还生产者ChannelWrapper到池中
func (p *Pool) Release(cw *channelWrapper) {
	if cw == nil || p.closed {
		return
	}
	p.channels <- cw
}

// Close 关闭所有资源
func (p *Pool) Close() {
	// 加锁
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	// 关闭go channel
	close(p.channels)
	// 逐个关闭amqp channels
	for cw := range p.channels {
		_ = cw.ch.Close()
	}
	_ = p.conn.Close()
}

// EnsureBaseTopology 仅声明基础交换机，队列由具体消费者声明，避免参数冲突
func (p *Pool) EnsureBaseTopology() error {
	ch, err := p.conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	const exchangeName = "seckill.exchange"
	if err := ch.ExchangeDeclare(exchangeName, "topic", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare exchange failed: %w", err)
	}
	logger.Info("Base MQ exchange ensured")
	return nil
}

// PublishAsync 使用池中通道进行异步发布（不等待确认）
func (p *Pool) PublishAsync(exchange, key string, body []byte) error {
	// 接收exchange 路由键和消息体

	// 获取一个通道
	cw := p.Acquire()
	defer p.Release(cw)
	// 在该通道上发送消息到rabbitMQ服务器
	return cw.ch.Publish(exchange, key, false, false, amqp.Publishing{
		ContentType:  "application/json",
		Body:         body,
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now(),
	})
}

// NewConsumerChannel 独立创建用于消费的连接与通道（不依赖生产者池）
func NewConsumerChannel(cfg *config.MQConfig, queue, bindKey, exchange string, durable bool, prefetch int) (*amqp.Connection, *amqp.Channel, <-chan amqp.Delivery, error) {
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
		// 确保交换机存在
		if err := ch.ExchangeDeclare(exchange, "topic", true, false, false, false, nil); err != nil {
			ch.Close()
			conn.Close()
			return nil, nil, nil, fmt.Errorf("declare exchange failed: %w", err)
		}
	}
	// 声明队列
	if _, err := ch.QueueDeclare(queue, durable, false, false, false, nil); err != nil {
		ch.Close()
		conn.Close()
		return nil, nil, nil, fmt.Errorf("declare queue failed: %w", err)
	}

	// 绑定队列到交换机
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

// CloseConsumer 关闭消费者连接与通道
func CloseConsumer(conn *amqp.Connection, ch *amqp.Channel) {
	if ch != nil {
		_ = ch.Close()
	}
	if conn != nil {
		_ = conn.Close()
	}
}
