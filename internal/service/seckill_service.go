package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/CCDD2022/seckill-system/internal/dao"
	"github.com/CCDD2022/seckill-system/proto_output/seckill"
	"github.com/redis/go-redis/v9"
	"github.com/streadway/amqp"
)

type SeckillService struct {
	productDao *dao.ProductDao
	redisDB    *redis.Client
	mq         *amqp.Channel
	seckill.UnimplementedSeckillServiceServer
}

func NewSeckillService(productDao *dao.ProductDao, redisDB *redis.Client, mq *amqp.Channel) *SeckillService {
	return &SeckillService{
		productDao: productDao,
		redisDB:    redisDB,
		mq:         mq,
	}
}

// SeckillMessage 秒杀订单消息
type SeckillMessage struct {
	UserID     int64   `json:"user_id"`
	ProductID  int64   `json:"product_id"`
	Quantity   int32   `json:"quantity"`
	TotalPrice float64 `json:"total_price"`
}

// ExecuteSeckill 执行秒杀
func (s *SeckillService) ExecuteSeckill(ctx context.Context, req *seckill.SeckillRequest) (*seckill.SeckillResponse, error) {
	productID := req.ProductId
	userID := req.UserId
	quantity := req.Quantity

	// 1. 使用分布式锁防止重复下单
	lockKey := fmt.Sprintf("seckill:lock:user:%d:product:%d", userID, productID)
	locked, err := s.redisDB.SetNX(ctx, lockKey, "1", 10*time.Second).Result()
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
	defer s.redisDB.Del(ctx, lockKey)

	// 2. 从Redis预扣减库存
	stockKey := fmt.Sprintf("product:stock:%d", productID)
	
	// Lua脚本保证原子性
	luaScript := `
		local stock = redis.call('GET', KEYS[1])
		if not stock or tonumber(stock) < tonumber(ARGV[1]) then
			return 0
		end
		redis.call('DECRBY', KEYS[1], ARGV[1])
		return 1
	`
	
	result, err := s.redisDB.Eval(ctx, luaScript, []string{stockKey}, quantity).Result()
	if err != nil {
		return &seckill.SeckillResponse{
			Success: false,
			Message: "扣减库存失败",
		}, err
	}

	if result.(int64) == 0 {
		return &seckill.SeckillResponse{
			Success: false,
			Message: "商品已售罄",
		}, nil
	}

	// 3. 获取商品信息计算总价
	product, err := s.productDao.GetProductByID(ctx, productID)
	if err != nil {
		// 回滚库存
		s.redisDB.IncrBy(ctx, stockKey, int64(quantity))
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
		s.redisDB.IncrBy(ctx, stockKey, int64(quantity))
		return &seckill.SeckillResponse{
			Success: false,
			Message: "创建订单消息失败",
		}, err
	}

	err = s.mq.Publish(
		"",              // exchange
		"order.create",  // routing key
		false,           // mandatory
		false,           // immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        msgBody,
		})

	if err != nil {
		// 回滚库存
		s.redisDB.IncrBy(ctx, stockKey, int64(quantity))
		return &seckill.SeckillResponse{
			Success: false,
			Message: "秒杀失败，请重试",
		}, err
	}

	return &seckill.SeckillResponse{
		Success: true,
		Message: "秒杀成功，订单处理中",
		OrderId: 0, // 订单ID将在异步处理后生成
	}, nil
}
