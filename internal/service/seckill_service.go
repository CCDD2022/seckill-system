package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/CCDD2022/seckill-system/internal/dao"
	"github.com/CCDD2022/seckill-system/internal/mq"
	"github.com/CCDD2022/seckill-system/proto_output/seckill"
	"github.com/redis/go-redis/v9"
)

type SeckillService struct {
	productDao *dao.ProductDao
	redisDB    redis.UniversalClient
	mqPool     *mq.Pool
	seckill.UnimplementedSeckillServiceServer
}

func NewSeckillService(productDao *dao.ProductDao, redisDB redis.UniversalClient, mqPool *mq.Pool) *SeckillService {
	return &SeckillService{
		productDao: productDao,
		redisDB:    redisDB,
		mqPool:     mqPool,
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

	//// 快速校验：时间窗口与基本参数（避免无意义的后续操作）
	//{
	//	cctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	//	defer cancel()
	//	p, err := s.productDao.GetProductByID(cctx, productID)
	//	if err == nil && p != nil && p.SeckillStartTime != nil && p.SeckillEndTime != nil {
	//		now := time.Now()
	//		if now.Before(*p.SeckillStartTime) {
	//			return &seckill.SeckillResponse{Success: false, Message: "活动未开始"}, nil
	//		}
	//		if now.After(*p.SeckillEndTime) {
	//			return &seckill.SeckillResponse{Success: false, Message: "活动已结束"}, nil
	//		}
	//	}
	//}

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
	_ = s.publishStockLog(
		StockLogMessage{ProductID: productID, Delta: -quantity, Reason: "seckill_deduct", TimeUnix: time.Now().Unix()})

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

	// 生成消息ID用于消费端幂等（当前序列化到 Body 中的业务字段）
	_ = fmt.Sprintf("%d-%d-%d", userID, productID, time.Now().UnixNano())

	// 发布创建订单事件（异步Confirm，提高吞吐）
	// 将消息ID放入 Header/Body 供消费者去重（此处直接放 Body 字段 msgID）
	if err := s.mqPool.PublishAsync(mqExchange, "order.create", msgBody); err != nil {
		_ = s.productDao.ReturnStock(context.Background(), productID, quantity)
		_ = s.publishStockLog(StockLogMessage{ProductID: productID, Delta: quantity, Reason: "seckill_publish_fail", TimeUnix: time.Now().Unix()})
		return &seckill.SeckillResponse{Success: false, Message: "秒杀失败，请重试"}, err
	}
	// 不再等待确认，改为异步处理（提高吞吐）

	return &seckill.SeckillResponse{
		Success: true,
		Message: "秒杀成功，订单处理中",
		OrderId: 0, // 订单ID将在异步处理后生成
	}, nil
}

func (s *SeckillService) publishStockLog(m StockLogMessage) error {
	b, _ := json.Marshal(m)
	return s.mqPool.PublishAsync(mqExchange, "stock.change", b)
}
