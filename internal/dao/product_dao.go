package dao

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/CCDD2022/seckill-system/internal/model"
	"github.com/CCDD2022/seckill-system/pkg/logger"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type ProductDao struct {
	db                *gorm.DB
	redis             redis.UniversalClient
	reservationStream string
}

var (
	ErrProductInUse           = errors.New("product has active or historical order references")
	ErrProductImmutable       = errors.New("campaign inventory, time and price are immutable")
	ErrProductCreateUncertain = errors.New("product creation outcome is uncertain")
)

func NewProductDao(db *gorm.DB, redis redis.UniversalClient) *ProductDao {
	return &ProductDao{
		db:                db,
		redis:             redis,
		reservationStream: ReservationStream,
	}
}

// 缓存相关常量
const (
	productStockKeyTemplate = "stock:%d"
	productCacheKeyTemplate = "product:%d"
	productPriceKeyTemplate = "product_price:%d"
	cacheExpiration         = 30 * time.Minute
	productDirtySetKey      = "product:dirty"
)

// getProductCacheKey 生成单个商品缓存键
func getProductCacheKey(id int64) string {
	return fmt.Sprintf(productCacheKeyTemplate, id)
}

// getProductStockKey 生成库存缓存键
func getProductStockKey(id int64) string {
	return fmt.Sprintf(productStockKeyTemplate, id)
}

// getProductPriceKey 生成价格缓存键
func getProductPriceKey(id int64) string {
	return fmt.Sprintf(productPriceKeyTemplate, id)
}

// GetProductByID 根据ID查询商品（带缓存）
func (dao *ProductDao) GetProductByID(ctx context.Context, id int64) (*model.Product, error) {
	cacheKey := getProductCacheKey(id)

	cachedData, err := dao.redis.Get(ctx, cacheKey).Result()
	// redis里不包含该键
	if errors.Is(err, redis.Nil) {
		var product model.Product
		// 从数据库里查询
		err = dao.db.WithContext(ctx).First(&product, "id = ?", id).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 数据库没找到该商品
			// 缓存空商品 防止后续多次访问空商品导致数据库压力过大
			emptyProduct := &model.Product{
				ID: 0,
			}
			cacheValue, _ := json.Marshal(emptyProduct)
			if err := dao.redis.Set(ctx, cacheKey, cacheValue, 5*time.Minute).Err(); err != nil {
				logger.Error("缓存写入失败", "key", cacheKey, "err", err)
			}
			return nil, err
		} else if err != nil {
			return nil, err
		}

		// 序列化 写入缓存
		if productJSON, marshalErr := json.Marshal(product); marshalErr == nil {
			dao.redis.Set(ctx, cacheKey, productJSON, cacheExpiration)
		}
		// 计算目前的秒杀状态 获取最新的返回给前端
		product.CalculateSeckillStatus()
		return &product, nil
	} else if err != nil {
		return nil, err
	}

	// 从缓存里反序列化
	var product model.Product

	// 解析错误 删除缓存并重试(即从数据库加载)
	if err := json.Unmarshal([]byte(cachedData), &product); err != nil {
		dao.redis.Del(ctx, cacheKey)
		return dao.GetProductByID(ctx, id)
	}

	// 是我们约定好的空商品
	if product.ID == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	product.CalculateSeckillStatus()
	return &product, nil
}

// CreateProduct 创建商品
func (dao *ProductDao) CreateProduct(ctx context.Context, product *model.Product) (int64, error) {
	if product.Stock < 0 {
		return 0, errors.New("初始库存不能为负数")
	}
	if (product.SeckillStartTime == nil) != (product.SeckillEndTime == nil) ||
		(product.SeckillStartTime != nil && !product.SeckillStartTime.Before(*product.SeckillEndTime)) {
		return 0, errors.New("秒杀活动时间无效")
	}
	if !model.ValidProductPrice(product.Price) {
		return 0, errors.New("商品价格无效")
	}
	seededStock := false
	readyToCommit := false
	err := dao.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(product).Error; err != nil {
			return err
		}
		// The product remains invisible to other DB readers until commit. Seed
		// Redis before committing so a failed seed also rolls back the row.
		ok, err := dao.redis.SetNX(ctx, getProductStockKey(product.ID), product.Stock, 0).Result()
		if err != nil {
			return fmt.Errorf("初始化库存失败: %w", err)
		}
		if !ok {
			return errors.New("新商品库存键已存在，拒绝覆盖")
		}
		seededStock = true
		if product.SeckillStartTime != nil {
			if err := dao.redis.HSet(ctx, campaignKey(product.ID),
				"active", 0,
				"price_cents", int64(math.Round(product.Price*100)),
				"start_unix", product.SeckillStartTime.Unix(),
				"end_unix", product.SeckillEndTime.Unix(),
			).Err(); err != nil {
				return fmt.Errorf("初始化活动元数据失败: %w", err)
			}
		}
		readyToCommit = true
		return nil
	})
	if err != nil {
		if !seededStock {
			return 0, err
		}
		verifyCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if !readyToCommit {
			// A callback failure happens before COMMIT; GORM rolled back the
			// transaction. Cleaning up the provisional Redis keys is safe.
			if cleanupErr := dao.redis.Del(verifyCtx, getProductStockKey(product.ID), campaignKey(product.ID)).Err(); cleanupErr != nil {
				return product.ID, fmt.Errorf("%w: product %d rolled back but Redis cleanup failed: %v", ErrProductCreateUncertain, product.ID, cleanupErr)
			}
			return 0, err
		}
		// The COMMIT response can be lost after MySQL committed. Query the
		// primary to recover a confirmed row. An uncertain result keeps the
		// campaign inactive so a guessed product ID cannot sell orphan stock.
		var persisted model.Product
		lookupErr := dao.db.WithContext(verifyCtx).Select("id").First(&persisted, "id = ?", product.ID).Error
		switch {
		case lookupErr == nil:
			if activateErr := dao.activateCampaign(product.ID, product.SeckillStartTime != nil); activateErr != nil {
				return product.ID, fmt.Errorf("%w: product %d committed but activation could not be verified: %v", ErrProductCreateUncertain, product.ID, activateErr)
			}
			return product.ID, nil
		case errors.Is(lookupErr, gorm.ErrRecordNotFound):
			// An immediate read does not prove an in-flight COMMIT will never
			// appear. Keep the Redis campaign inactive for later reconciliation.
			return product.ID, fmt.Errorf("%w: product %d was not visible after ambiguous COMMIT", ErrProductCreateUncertain, product.ID)
		default:
			logger.Error("product commit outcome could not be verified; campaign remains inactive", "product_id", product.ID, "err", lookupErr)
			return product.ID, fmt.Errorf("%w: product %d needs reconciliation", ErrProductCreateUncertain, product.ID)
		}
	}
	if err := dao.activateCampaign(product.ID, product.SeckillStartTime != nil); err != nil {
		return product.ID, fmt.Errorf("%w: product %d committed but campaign activation failed: %v", ErrProductCreateUncertain, product.ID, err)
	}
	return product.ID, nil
}

func (dao *ProductDao) activateCampaign(productID int64, campaignExpected bool) error {
	if !campaignExpected {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	exists, err := dao.redis.Exists(ctx, campaignKey(productID)).Result()
	if err != nil {
		return err
	}
	if exists == 0 {
		return errors.New("campaign metadata is missing")
	}
	return dao.redis.HSet(ctx, campaignKey(productID), "active", 1).Err()
}

// DeleteProductByID 删除商品
func (dao *ProductDao) DeleteProductByID(ctx context.Context, id int64) error {
	var product model.Product
	if err := dao.db.WithContext(ctx).Select("id", "seckill_start_time").First(&product, "id = ?", id).Error; err != nil {
		return err
	}
	if product.SeckillStartTime != nil {
		return fmt.Errorf("%w: 秒杀商品需保留以供订单和库存审计", ErrProductInUse)
	}
	// A reservation can still be waiting in Redis or RabbitMQ when there is no
	// order row yet. Keep the product until both accepted work and orders are
	// absent, otherwise cancellation/reconciliation loses its target.
	participants, err := dao.redis.SCard(ctx, fmt.Sprintf("seckill:joined:product:%d", id)).Result()
	if err != nil {
		return fmt.Errorf("检查预占记录失败: %w", err)
	}
	if participants > 0 {
		return fmt.Errorf("%w: 商品已有秒杀预占记录", ErrProductInUse)
	}
	var orders int64
	if err := dao.db.WithContext(ctx).Model(&model.Order{}).Where("product_id = ?", id).Count(&orders).Error; err != nil {
		return err
	}
	if orders > 0 {
		return fmt.Errorf("%w: 商品已有订单", ErrProductInUse)
	}
	if err := dao.db.WithContext(ctx).Delete(&model.Product{}, id).Error; err != nil {
		return err
	}
	dao.ClearProductCache(ctx, id)
	_ = dao.redis.Del(ctx, getProductStockKey(id), campaignKey(id)).Err()
	return nil
}

// UpdateProduct 更新商品
func (dao *ProductDao) UpdateProduct(ctx context.Context, id int64, updates map[string]interface{}) error {
	if _, stockChange := updates["stock"]; stockChange {
		return ErrProductImmutable
	}
	if _, change := updates["seckill_start_time"]; change {
		return ErrProductImmutable
	}
	if _, change := updates["seckill_end_time"]; change {
		return ErrProductImmutable
	}
	if _, change := updates["price"]; change {
		var current model.Product
		if err := dao.db.WithContext(ctx).Select("id", "seckill_start_time").First(&current, "id = ?", id).Error; err != nil {
			return err
		}
		if current.SeckillStartTime != nil {
			return ErrProductImmutable
		}
	}
	if err := dao.db.WithContext(ctx).Model(&model.Product{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return err
	}
	dao.ClearProductCache(ctx, id)
	return nil
}

// ListProductsFromDBWithStatus 从数据库分页查询商品，支持状态筛选（-1 表示全部）
func (dao *ProductDao) ListProductsFromDBWithStatus(ctx context.Context, offset, limit int32, status int32) ([]*model.Product, int64, error) {
	var products []*model.Product
	query := dao.db.WithContext(ctx).Model(&model.Product{})
	// 统计总数
	if status >= 0 && status <= 2 {
		query = dao.applyStatusFilter(query, model.ProductSeckillStatus(status))
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	// 分页查询
	if err := query.Offset(int(offset)).Limit(int(limit)).Find(&products).Error; err != nil {
		return nil, 0, err
	}
	for i := range products {
		products[i].CalculateSeckillStatus()
	}
	return products, total, nil
}

// applyStatusFilter 应用状态筛选条件
func (dao *ProductDao) applyStatusFilter(query *gorm.DB, statusFilter model.ProductSeckillStatus) *gorm.DB {
	now := time.Now().Format(time.DateTime)

	switch statusFilter {
	case model.SeckillStatusActive:
		return query.Where("seckill_start_time <= ? AND seckill_end_time >= ? AND stock > 0", now, now)
	case model.SeckillStatusNotStarted:
		return query.Where("seckill_start_time > ?", now)
	case model.SeckillStatusEnded:
		return query.Where("seckill_end_time < ? OR stock <= 0", now)
	default:
		return query
	}
}

// ClearProductCache 清理商品缓存
func (dao *ProductDao) ClearProductCache(ctx context.Context, id int64) {
	cacheKey := getProductCacheKey(id)
	priceKey := getProductPriceKey(id)
	// 同步删除商品详情与价格小Key，保持一致性
	dao.redis.Del(ctx, cacheKey, priceKey)
}

// GetProductPrice 轻量获取商品价格（优先Redis，小Key，避免反序列化整对象）
func (dao *ProductDao) GetProductPrice(ctx context.Context, id int64) (float64, error) {
	priceKey := getProductPriceKey(id)
	if val, err := dao.redis.Get(ctx, priceKey).Result(); err == nil {
		if f, convErr := strconv.ParseFloat(val, 64); convErr == nil {
			return f, nil
		}
		// 解析失败则删除键，走DB
		_ = dao.redis.Del(ctx, priceKey).Err()
	}

	// 从数据库仅查询价格
	var p model.Product
	if err := dao.db.WithContext(ctx).Select("id", "price").First(&p, "id = ?", id).Error; err != nil {
		return 0, err
	}
	// 回写价格缓存，较长TTL（价格非高频变动），具体变更时由更新路径清缓存或重置
	_ = dao.redis.Set(ctx, priceKey, fmt.Sprintf("%f", p.Price), 20*time.Minute).Err()
	return p.Price, nil
}
