package dao

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/CCDD2022/seckill-system/internal/model"
	"github.com/redis/go-redis/v9"
)

type CampaignSnapshot struct {
	PriceCents int64
	StartUnix  int64
	EndUnix    int64
}

func campaignKey(productID int64) string {
	return fmt.Sprintf("seckill:campaign:%d", productID)
}

// GetCampaignSnapshot uses immutable Redis metadata on the hot path. Legacy
// products created before this cache was introduced are initialized once from
// MySQL; campaign price and time cannot be edited after product creation.
func (dao *ProductDao) GetCampaignSnapshot(ctx context.Context, productID int64) (CampaignSnapshot, error) {
	key := campaignKey(productID)
	values, err := dao.redis.HMGet(ctx, key, "active", "price_cents", "start_unix", "end_unix").Result()
	if err != nil {
		return CampaignSnapshot{}, err
	}
	if values[0] != nil && fmt.Sprint(values[0]) == "1" {
		if values[1] == nil || values[2] == nil || values[3] == nil {
			return CampaignSnapshot{}, errors.New("秒杀活动缓存不完整")
		}
		price, err1 := strconv.ParseInt(fmt.Sprint(values[1]), 10, 64)
		start, err2 := strconv.ParseInt(fmt.Sprint(values[2]), 10, 64)
		end, err3 := strconv.ParseInt(fmt.Sprint(values[3]), 10, 64)
		if err1 != nil || err2 != nil || err3 != nil || price <= 0 || start >= end {
			return CampaignSnapshot{}, errors.New("秒杀活动缓存无效")
		}
		return CampaignSnapshot{PriceCents: price, StartUnix: start, EndUnix: end}, nil
	}
	if values[0] != nil && fmt.Sprint(values[0]) != "0" {
		return CampaignSnapshot{}, errors.New("秒杀活动激活状态无效")
	}
	// An inactive campaign may belong to a transaction whose COMMIT outcome
	// was uncertain. Only a fresh read from MySQL may activate it. The same
	// path repairs metadata created by older versions without an active flag.
	if dao.db == nil {
		return CampaignSnapshot{}, ErrCampaignNotConfigured
	}
	var product model.Product
	if err := dao.db.WithContext(ctx).
		Select("id", "price", "seckill_start_time", "seckill_end_time").
		First(&product, "id = ?", productID).Error; err != nil {
		return CampaignSnapshot{}, err
	}
	if product.SeckillStartTime == nil || product.SeckillEndTime == nil ||
		!product.SeckillStartTime.Before(*product.SeckillEndTime) ||
		!model.ValidProductPrice(product.Price) {
		return CampaignSnapshot{}, ErrCampaignNotConfigured
	}
	snapshot := CampaignSnapshot{
		PriceCents: int64(math.Round(product.Price * 100)),
		StartUnix:  product.SeckillStartTime.Unix(),
		EndUnix:    product.SeckillEndTime.Unix(),
	}
	if err := dao.redis.HSet(ctx, key,
		"active", 1,
		"price_cents", snapshot.PriceCents,
		"start_unix", snapshot.StartUnix,
		"end_unix", snapshot.EndUnix,
	).Err(); err != nil {
		return CampaignSnapshot{}, err
	}
	return snapshot, nil
}

// The stock reservation and its durable handoff are one Redis operation. The
// deployment uses one persistent Redis instance; these keys are not designed
// for a multi-slot Redis Cluster.
const (
	ReservationStream = "seckill:orders:outbox"
	ReservationGroup  = "seckill:order-relay"
)

var (
	ErrDuplicateReservation   = errors.New("该商品已参与过秒杀")
	ErrSoldOut                = errors.New("库存不足")
	ErrStockNotInitialized    = errors.New("商品库存尚未初始化")
	ErrStockStateInvalid      = errors.New("库存状态异常")
	ErrReservationUncertain   = errors.New("秒杀预占结果尚未确认")
	ErrEventPersistenceFailed = errors.New("订单事件无法持久化")
	ErrCampaignNotConfigured  = errors.New("商品没有有效的秒杀活动")
	ErrCampaignNotActive      = errors.New("当前不在秒杀活动时间内")
)

var reserveAndEnqueue = redis.NewScript(`
local stock = redis.call('GET', KEYS[1])
if not stock then return -1 end
if not tonumber(stock) then return -5 end
local startUnix = redis.call('HGET', KEYS[6], 'start_unix')
local endUnix = redis.call('HGET', KEYS[6], 'end_unix')
local priceCents = redis.call('HGET', KEYS[6], 'price_cents')
local active = redis.call('HGET', KEYS[6], 'active')
if active ~= '1' or not startUnix or not endUnix or not priceCents then return -6 end
if priceCents ~= ARGV[6] then return -6 end
local nowUnix = tonumber(redis.call('TIME')[1])
if nowUnix < tonumber(startUnix) or nowUnix >= tonumber(endUnix) then return -7 end
local joinType = redis.call('TYPE', KEYS[2]).ok
local dirtyType = redis.call('TYPE', KEYS[3]).ok
if (joinType ~= 'none' and joinType ~= 'set') or (dirtyType ~= 'none' and dirtyType ~= 'set') then return -5 end
if redis.call('SISMEMBER', KEYS[2], ARGV[1]) == 1 then return -3 end
if tonumber(stock) < tonumber(ARGV[2]) then return -2 end

-- Do the set allocations before XADD. A failed XADD rolls back participation;
-- the harmless dirty marker may be reconciled to the unchanged stock.
redis.call('SADD', KEYS[2], ARGV[1])
redis.call('SADD', KEYS[3], ARGV[3])
local appended = redis.pcall('XADD', KEYS[4], '*', 'message_id', ARGV[4], 'payload', ARGV[5])
if type(appended) == 'table' and appended.err then
  redis.call('SREM', KEYS[2], ARGV[1])
  return -4
end
local deducted = redis.pcall('DECRBY', KEYS[1], ARGV[2])
if type(deducted) == 'table' and deducted.err then
  redis.call('XDEL', KEYS[4], appended)
  redis.call('SREM', KEYS[2], ARGV[1])
  return -5
end
redis.call('DEL', KEYS[5])
return 1
`)

var returnOnce = redis.NewScript(`
if redis.call('SISMEMBER', KEYS[2], ARGV[1]) == 1 then return 0 end
local stock = redis.call('GET', KEYS[1])
if not stock then return -1 end
if not tonumber(stock) then return -2 end
local processedType = redis.call('TYPE', KEYS[2]).ok
local dirtyType = redis.call('TYPE', KEYS[3]).ok
if (processedType ~= 'none' and processedType ~= 'set') or (dirtyType ~= 'none' and dirtyType ~= 'set') then return -2 end
redis.call('SADD', KEYS[3], ARGV[3])
redis.call('SADD', KEYS[2], ARGV[1])
local restored = redis.pcall('INCRBY', KEYS[1], ARGV[2])
if type(restored) == 'table' and restored.err then
  redis.call('SREM', KEYS[2], ARGV[1])
  return -2
end
redis.call('DEL', KEYS[4])
return 1
`)

// ReserveStockAndEnqueue reserves stock, prevents a second purchase by the
// same user, and records the order event for the relay in one Redis script.
// The caller must validate the product and campaign window before calling.
func (dao *ProductDao) ReserveStockAndEnqueue(ctx context.Context, productID, userID int64, quantity int32, priceCents int64, messageID string, payload []byte) error {
	if productID <= 0 || userID <= 0 || quantity <= 0 || priceCents <= 0 || messageID == "" || len(payload) == 0 {
		return errors.New("无效的秒杀请求")
	}
	stream := dao.reservationStream
	if stream == "" {
		stream = ReservationStream
	}
	keys := []string{
		getProductStockKey(productID),
		fmt.Sprintf("seckill:joined:product:%d", productID),
		productDirtySetKey,
		stream,
		getProductCacheKey(productID),
		campaignKey(productID),
	}
	result, err := reserveAndEnqueue.Run(ctx, dao.redis, keys, userID, quantity, productID, messageID, string(payload), priceCents).Int64()
	if err != nil {
		// A transport timeout may occur after Redis has already committed the
		// script. The caller must never describe that outcome as a definite
		// failure or compensate stock automatically.
		return fmt.Errorf("%w: %v", ErrReservationUncertain, err)
	}
	switch result {
	case 1:
		return nil
	case -1:
		return ErrStockNotInitialized
	case -2:
		return ErrSoldOut
	case -3:
		return ErrDuplicateReservation
	case -4:
		return ErrEventPersistenceFailed
	case -5:
		return ErrStockStateInvalid
	case -6:
		return ErrCampaignNotConfigured
	case -7:
		return ErrCampaignNotActive
	default:
		return fmt.Errorf("未知库存预占结果: %s", strconv.FormatInt(result, 10))
	}
}

// ReturnStockOnce makes cancellation idempotent even if RabbitMQ redelivers an
// event after the Redis operation succeeded but before the broker ACK.
func (dao *ProductDao) ReturnStockOnce(ctx context.Context, productID int64, quantity int32, eventID string) error {
	if productID <= 0 || quantity <= 0 || eventID == "" {
		return errors.New("无效的库存归还事件")
	}
	keys := []string{
		getProductStockKey(productID),
		"seckill:cancel:processed",
		productDirtySetKey,
		getProductCacheKey(productID),
	}
	result, err := returnOnce.Run(ctx, dao.redis, keys, eventID, quantity, productID).Int64()
	if err != nil {
		return fmt.Errorf("归还库存失败: %w", err)
	}
	if result == -1 {
		return ErrStockNotInitialized
	}
	if result != 0 && result != 1 {
		return fmt.Errorf("%w: 归还结果 %d", ErrStockStateInvalid, result)
	}
	return nil
}
