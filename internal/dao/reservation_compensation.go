package dao

import (
	"context"
	"errors"
	"fmt"

	"github.com/redis/go-redis/v9"
)

const compensatedReservationsKey = "seckill:orders:compensated"

var (
	ErrReservationNotFound      = errors.New("秒杀预占标记不存在，拒绝补偿以免虚增库存")
	ErrCompensationStateInvalid = errors.New("补偿所需 Redis 状态异常")
)

// The marker and stock increment happen in one Redis script. The joined marker
// is deliberately retained: a compensated failed order does not grant another
// chance to reserve the same product without explicit operator intervention.
var compensateFailedReservation = redis.NewScript(`
if redis.call('TYPE', KEYS[2]).ok ~= 'none' and redis.call('TYPE', KEYS[2]).ok ~= 'set' then return -3 end
if redis.call('SISMEMBER', KEYS[2], ARGV[1]) == 1 then return 0 end
local stock = redis.call('GET', KEYS[1])
if not stock then return -1 end
if not string.match(stock, '^%d+$') then return -3 end
local joinType = redis.call('TYPE', KEYS[3]).ok
local dirtyType = redis.call('TYPE', KEYS[4]).ok
if joinType == 'none' then return -2 end
if (joinType ~= 'set') or (dirtyType ~= 'none' and dirtyType ~= 'set') then return -3 end
if redis.call('SISMEMBER', KEYS[3], ARGV[2]) ~= 1 then return -2 end
redis.call('SADD', KEYS[4], ARGV[3])
redis.call('SADD', KEYS[2], ARGV[1])
local restored = redis.pcall('INCRBY', KEYS[1], ARGV[4])
if type(restored) == 'table' and restored.err then
  redis.call('SREM', KEYS[2], ARGV[1])
  return -3
end
redis.call('DEL', KEYS[5])
return 1
`)

func (dao *ProductDao) IsReservationCompensated(ctx context.Context, messageID string) (bool, error) {
	if messageID == "" {
		return false, errors.New("empty reservation message ID")
	}
	return dao.redis.SIsMember(ctx, compensatedReservationsKey, messageID).Result()
}

// CompensateFailedReservation is only for an operator after separately
// verifying the archived message, stopped consumers/relay, absent order, empty
// RabbitMQ queue, and no matching Redis Stream entry. It is idempotent.
func (dao *ProductDao) CompensateFailedReservation(ctx context.Context, productID, userID int64, quantity int32, messageID string) (bool, error) {
	if productID <= 0 || userID <= 0 || quantity <= 0 || messageID == "" {
		return false, errors.New("invalid reservation compensation")
	}
	keys := []string{
		getProductStockKey(productID),
		compensatedReservationsKey,
		fmt.Sprintf("seckill:joined:product:%d", productID),
		productDirtySetKey,
		getProductCacheKey(productID),
	}
	result, err := compensateFailedReservation.Run(ctx, dao.redis, keys, messageID, userID, productID, quantity).Int64()
	if err != nil {
		return false, fmt.Errorf("atomic reservation compensation failed: %w", err)
	}
	switch result {
	case 1:
		return true, nil
	case 0:
		return false, nil
	case -1:
		return false, ErrStockNotInitialized
	case -2:
		return false, ErrReservationNotFound
	default:
		return false, ErrCompensationStateInvalid
	}
}
