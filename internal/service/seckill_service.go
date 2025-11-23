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

	// 1. 使用参与集合去重，避免SetNX+Del的两次往返
	//    SADD 返回1表示首次参与，0表示已参与
	//    在后续失败（发布消息或其他环节）时再进行SREM，允许重试
	joinKey := fmt.Sprintf("seckill:joined:product:%d", productID)
	jctx, jcancel := context.WithTimeout(ctx, 80*time.Millisecond)
	defer jcancel()
	added, err := s.redisDB.SAdd(jctx, joinKey, userID).Result()
	s.redisDB.Expire(jctx, joinKey, 24*time.Hour)
	if err != nil {
		return &seckill.SeckillResponse{Success: false, Message: "系统繁忙，请稍后再试"}, err
	}
	if added == 0 {
		return &seckill.SeckillResponse{Success: false, Message: "您已参与过该商品的秒杀，请勿重复下单"}, nil
	}
	// 仅在需要允许重试的失败场景下移除参与标记
	removeJoinMark := func() {
		c, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		_ = s.redisDB.SRem(c, joinKey, userID).Err()
	}

	// 2. 预扣减库存（统一走DAO的Lua脚本，保证键名一致与行为一致）
	if err := s.productDao.DeductStock(ctx, productID, quantity); err != nil {
		// 库存失败，移除参与标记，允许用户重试
		removeJoinMark()
		return &seckill.SeckillResponse{Success: false, Message: err.Error()}, nil
	}

	// 2.1 发送库存变更日志（非关键，不影响主流程）
	// _ = s.publishStockLog(
	// 		StockLogMessage{ProductID: productID, Delta: -quantity, Reason: "seckill_deduct", TimeUnix: time.Now().Unix()})

	// 3. 获取商品信息计算总价
	pctx, pcancel := context.WithTimeout(ctx, 120*time.Millisecond)
	price, err := s.productDao.GetProductPrice(pctx, productID)
	pcancel()
	if err != nil {
		// 回滚库存（DAO归还保证一致键名与逻辑）
		_ = s.productDao.ReturnStock(context.Background(), productID, quantity)
		// 获取价格失败，移除参与标记，允许重试
		removeJoinMark()
		return &seckill.SeckillResponse{
			Success: false,
			Message: "获取商品信息失败",
		}, err
	}
	totalPrice := price * float64(quantity)

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
		// 允许重试
		removeJoinMark()
		return &seckill.SeckillResponse{
			Success: false,
			Message: "创建订单消息失败",
		}, err
	}

	// 生成 MessageId（用于消费者 Redis 幂等 SetNX）
	// 尽量包含业务语义前缀，便于排查
	msgID := fmt.Sprintf("create:%d:%d:%d", userID, productID, time.Now().UnixNano())

	// 发布创建订单事件（异步Confirm，提高吞吐），携带 MessageId
	if err := s.mqPool.PublishAsyncWithID(mqExchange, "order.create", msgBody, msgID); err != nil {
		_ = s.productDao.ReturnStock(context.Background(), productID, quantity)
		// 发布失败，允许重试
		removeJoinMark()
		// _ = s.publishStockLog(StockLogMessage{ProductID: productID, Delta: quantity, Reason: "seckill_publish_fail", TimeUnix: time.Now().Unix()})
		return &seckill.SeckillResponse{Success: false, Message: "秒杀失败，请重试"}, err
	}
	// 不再等待确认，改为异步处理（提高吞吐）

	return &seckill.SeckillResponse{
		Success: true,
		Message: "秒杀成功，订单处理中",
		OrderId: 0, // 订单ID将在异步处理后生成
	}, nil
}

//
//func (s *SeckillService) publishStockLog(m StockLogMessage) error {
//	b, _ := json.Marshal(m)
//	return s.mqPool.PublishAsync(mqExchange, "stock.change", b)
//}
