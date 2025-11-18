package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/CCDD2022/seckill-system/internal/dao"
	"github.com/CCDD2022/seckill-system/proto_output/seckill"
	"github.com/redis/go-redis/v9"
	"github.com/streadway/amqp"
)

type SeckillService struct {
	productDao *dao.ProductDao
	redisDB    redis.UniversalClient
	mqChannels []pubChannel
	rrCounter  uint64
	seckill.UnimplementedSeckillServiceServer
}

type pubChannel struct {
	ch       *amqp.Channel
	confirms <-chan amqp.Confirmation
}

func NewSeckillService(productDao *dao.ProductDao, redisDB redis.UniversalClient, rawChannels []*amqp.Channel) *SeckillService {
	pcs := make([]pubChannel, 0, len(rawChannels))
	for _, ch := range rawChannels {
		// Ensure confirm mode enabled per channel
		_ = ch.Confirm(false)
		conf := ch.NotifyPublish(make(chan amqp.Confirmation, 1))
		pcs = append(pcs, pubChannel{ch: ch, confirms: conf})
	}
	return &SeckillService{
		productDao: productDao,
		redisDB:    redisDB,
		mqChannels: pcs,
	}
}

// SeckillMessage 发送到rabbitMQ的秒杀消息结构体
type SeckillMessage struct {
	UserID     int64   `json:"user_id"`
	ProductID  int64   `json:"product_id"`
	Quantity   int32   `json:"quantity"`
	TotalPrice float64 `json:"total_price"`
}

// StockLogMessage 库存变更日志消息（审计/可选）
type StockLogMessage struct {
	ProductID int64  `json:"product_id"`
	Delta     int32  `json:"delta"`
	Reason    string `json:"reason"`
	TimeUnix  int64  `json:"time_unix"`
}

const mqExchange = "seckill.exchange"

// ExecuteSeckill 执行秒杀
func (s *SeckillService) ExecuteSeckill(ctx context.Context, req *seckill.SeckillRequest) (*seckill.SeckillResponse, error) {
	productID := req.ProductId
	userID := req.UserId
	quantity := req.Quantity

	// 快速校验：时间窗口与基本参数（避免无意义的后续操作）
	{
		cctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()
		p, err := s.productDao.GetProductByID(cctx, productID)
		if err == nil && p != nil && p.SeckillStartTime != nil && p.SeckillEndTime != nil {
			now := time.Now()
			if now.Before(*p.SeckillStartTime) {
				return &seckill.SeckillResponse{Success: false, Message: "活动未开始"}, nil
			}
			if now.After(*p.SeckillEndTime) {
				return &seckill.SeckillResponse{Success: false, Message: "活动已结束"}, nil
			}
		}
	}

	// 1. 使用分布式锁（防重复下单）；短超时，避免阻塞
	// userid + productid 作为锁的key 防止重复下单
	lockKey := fmt.Sprintf("seckill:lock:user:%d:product:%d", userID, productID)
	lctx, lcancel := context.WithTimeout(ctx, 80*time.Millisecond)
	defer lcancel()
	locked, err := s.redisDB.SetNX(lctx, lockKey, "1", 10*time.Second).Result()
	if err != nil {
		return &seckill.SeckillResponse{
			Success: false,
			Message: "系统繁忙，请稍后再试",
		}, err
	}
	if !locked {
		return &seckill.SeckillResponse{
			Success: false,
			Message: "您已参与过该商品的秒杀，请勿重复下单",
		}, nil
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		_ = s.redisDB.Del(c, lockKey).Err()
	}()

	// 2. 预扣减库存（统一走DAO的Lua脚本，保证键名一致与行为一致）
	if err := s.productDao.DeductStock(ctx, productID, quantity); err != nil {
		return &seckill.SeckillResponse{Success: false, Message: err.Error()}, nil
	}

	// 2.1 发送库存变更日志（非关键，不影响主流程）
	_ = s.publishStockLog(StockLogMessage{ProductID: productID, Delta: -quantity, Reason: "seckill_deduct", TimeUnix: time.Now().Unix()})

	// 3. 获取商品信息计算总价
	pctx, pcancel := context.WithTimeout(ctx, 150*time.Millisecond)
	product, err := s.productDao.GetProductByID(pctx, productID)
	pcancel()
	if err != nil {
		// 回滚库存（DAO归还保证一致键名与逻辑）
		_ = s.productDao.ReturnStock(context.Background(), productID, quantity)
		return &seckill.SeckillResponse{
			Success: false,
			Message: "获取商品信息失败",
		}, err
	}

	totalPrice := product.Price * float64(quantity)

	// 4. 发送消息到队列进行异步订单创建
	msg := SeckillMessage{
		UserID:     userID,
		ProductID:  productID,
		Quantity:   quantity,
		TotalPrice: totalPrice,
	}

	msgBody, err := json.Marshal(msg)
	if err != nil {
		// 回滚库存
		_ = s.productDao.ReturnStock(context.Background(), productID, quantity)
		return &seckill.SeckillResponse{
			Success: false,
			Message: "创建订单消息失败",
		}, err
	}

	pch := s.pickChannel()
	if pch == nil {
		_ = s.productDao.ReturnStock(context.Background(), productID, quantity)
		_ = s.publishStockLog(StockLogMessage{ProductID: productID, Delta: quantity, Reason: "seckill_no_channel", TimeUnix: time.Now().Unix()})
		return &seckill.SeckillResponse{Success: false, Message: "系统繁忙，请重试"}, fmt.Errorf("no mq channel available")
	}

	// 生成消息ID用于消费端幂等
	msgID := fmt.Sprintf("%d-%d-%d", userID, productID, time.Now().UnixNano())

	publish := amqp.Publishing{
		ContentType:  "application/json",
		Body:         msgBody,
		MessageId:    msgID,
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now(),
	}
	if err := pch.ch.Publish(mqExchange, "order.create", false, false, publish); err != nil {
		_ = s.productDao.ReturnStock(context.Background(), productID, quantity)
		_ = s.publishStockLog(StockLogMessage{ProductID: productID, Delta: quantity, Reason: "seckill_publish_fail", TimeUnix: time.Now().Unix()})
		return &seckill.SeckillResponse{Success: false, Message: "秒杀失败，请重试"}, err
	}

	// 同步等待发布确认（每通道顺序确认）
	select {
	case cf := <-pch.confirms:
		if !cf.Ack {
			_ = s.productDao.ReturnStock(context.Background(), productID, quantity)
			_ = s.publishStockLog(StockLogMessage{ProductID: productID, Delta: quantity, Reason: "seckill_publish_nack", TimeUnix: time.Now().Unix()})
			return &seckill.SeckillResponse{Success: false, Message: "秒杀失败，请重试"}, fmt.Errorf("publish not acked")
		}
	case <-time.After(300 * time.Millisecond):
		// 超时视为失败，回滚
		_ = s.productDao.ReturnStock(context.Background(), productID, quantity)
		_ = s.publishStockLog(StockLogMessage{ProductID: productID, Delta: quantity, Reason: "seckill_publish_timeout", TimeUnix: time.Now().Unix()})
		return &seckill.SeckillResponse{Success: false, Message: "系统繁忙，请重试"}, fmt.Errorf("publish confirm timeout")
	}

	return &seckill.SeckillResponse{
		Success: true,
		Message: "秒杀成功，订单处理中",
		OrderId: 0, // 订单ID将在异步处理后生成
	}, nil
}

func (s *SeckillService) pickChannel() *pubChannel {
	if len(s.mqChannels) == 0 {
		return nil
	}
	i := atomic.AddUint64(&s.rrCounter, 1)
	return &s.mqChannels[int(i)%len(s.mqChannels)]
}

func (s *SeckillService) publishStockLog(m StockLogMessage) error {
	pch := s.pickChannel()
	if pch == nil {
		return fmt.Errorf("no mq channel available")
	}
	b, _ := json.Marshal(m)
	return pch.ch.Publish(
		mqExchange,
		"stock.change",
		false,
		false,
		amqp.Publishing{ContentType: "application/json", Body: b, DeliveryMode: amqp.Persistent, Timestamp: time.Now()},
	)
}
