package dao

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/CCDD2022/seckill-system/internal/model"
	"github.com/CCDD2022/seckill-system/pkg/logger"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type ProductDao struct {
	db    *gorm.DB
	redis *redis.Client
}

func NewProductDao(db *gorm.DB, redis *redis.Client) *ProductDao {
	return &ProductDao{
		db:    db,
		redis: redis,
	}
}

// 缓存相关常量
const (
	productStockKeyPrefix = "product:stock:product:id"
	productCacheKeyPrefix = "product:cache:id:"
	cacheExpiration       = 30 * time.Minute
)

// getProductCacheKey 生成单个商品缓存键
func getProductCacheKey(id int64) string {
	return productCacheKeyPrefix + strconv.FormatInt(id, 10)
}

// getProductStockKey 生成库存缓存键
func getProductStockKey(id int64) string {
	return productStockKeyPrefix + strconv.FormatInt(id, 10)
}

// GetProductByID 根据ID查询商品（带缓存）
func (dao *ProductDao) GetProductByID(ctx context.Context, id int64) (*model.Product, error) {
	cacheKey := getProductCacheKey(id)

	cachedData, err := dao.redis.Get(ctx, cacheKey).Result()
	if errors.Is(err, redis.Nil) {
		var product model.Product
		err = dao.db.WithContext(ctx).First(&product, "id = ?", id).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			emptyProduct := &model.Product{}
			cacheValue, _ := json.Marshal(emptyProduct)
			if err := dao.redis.Set(ctx, cacheKey, cacheValue, 5*time.Minute).Err(); err != nil {
				logger.Info("缓存写入失败", "key", cacheKey, "err", err)
			}
			return nil, err
		} else if err != nil {
			return nil, err
		}

		if productJSON, marshalErr := json.Marshal(product); marshalErr == nil {
			dao.redis.Set(ctx, cacheKey, productJSON, cacheExpiration)
		}

		product.CalculateSeckillStatus()
		return &product, nil
	} else if err != nil {
		return nil, err
	}

	var product model.Product
	if err := json.Unmarshal([]byte(cachedData), &product); err != nil {
		dao.redis.Del(ctx, cacheKey)
		return dao.GetProductByID(ctx, id)
	}

	if product.ID == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	product.CalculateSeckillStatus()
	return &product, nil
}

// CreateProduct 创建商品
func (dao *ProductDao) CreateProduct(ctx context.Context, product *model.Product) (int64, error) {
	err := dao.db.WithContext(ctx).Create(product).Error
	if err != nil {
		return 0, err
	}
	return product.ID, nil
}

// DeleteProductByID 删除商品
func (dao *ProductDao) DeleteProductByID(ctx context.Context, id int64) error {
	return dao.db.WithContext(ctx).Delete(&model.Product{}, id).Error
}

// UpdateProduct 更新商品
func (dao *ProductDao) UpdateProduct(ctx context.Context, id int64, updates map[string]interface{}) error {
	return dao.db.WithContext(ctx).Model(&model.Product{}).Where("id = ?", id).Updates(updates).Error
}

// GetTotalProducts 获取商品总数
func (dao *ProductDao) GetTotalProducts(ctx context.Context) (int64, error) {
	var total int64
	err := dao.db.WithContext(ctx).Model(&model.Product{}).Count(&total).Error
	return total, err
}

// GetTotalProductsByStatus 根据状态获取商品总数
func (dao *ProductDao) GetTotalProductsByStatus(ctx context.Context, statusFilter model.ProductSeckillStatus) (int64, error) {
	var total int64
	query := dao.db.WithContext(ctx).Model(&model.Product{})
	query = dao.applyStatusFilter(query, statusFilter)
	err := query.Count(&total).Error
	return total, err
}

// ListProductsFromDB 从数据库查询分页商品列表（支持状态筛选）
func (dao *ProductDao) ListProductsFromDB(ctx context.Context, offset, limit int32, statusFilter model.ProductSeckillStatus) ([]*model.Product, error) {
	var products []*model.Product
	query := dao.db.WithContext(ctx)
	query = dao.applyStatusFilter(query, statusFilter)

	err := query.Offset(int(offset)).Limit(int(limit)).Find(&products).Error
	if err != nil {
		return nil, err
	}

	for i := range products {
		products[i].CalculateSeckillStatus()
	}

	return products, nil
}

// applyStatusFilter 应用状态筛选条件
func (dao *ProductDao) applyStatusFilter(query *gorm.DB, statusFilter model.ProductSeckillStatus) *gorm.DB {
	now := time.Now()

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
	dao.redis.Del(ctx, cacheKey)
}

// DeductStock 优化 - Lua脚本返回状态码，避免额外Redis调用
func (dao *ProductDao) DeductStock(ctx context.Context, productID int64, quantity int32) error {
	redisKey := getProductStockKey(productID)

	luaScript := `
        local stock = redis.call('get', KEYS[1])
        if not stock then
            return -1  -- 键不存在
        end
        
        local stockNum = tonumber(stock)
        local quantity = tonumber(ARGV[1])
        
        if stockNum < quantity then
            return -2  -- 库存不足
        end
        
        redis.call('decrby', KEYS[1], quantity)
        return stockNum - quantity  -- 成功，返回新库存值
    `

	result, err := dao.redis.Eval(ctx, luaScript, []string{redisKey}, quantity).Result()
	if err != nil {
		return fmt.Errorf("redis执行失败: %w", err)
	}

	stockResult := result.(int64)
	switch stockResult {
	case -1:
		// 键不存在，安全预热后重试
		logger.Warn("库存键不存在，尝试预热", "product_id", productID)
		return dao.safeInitStockAndDeduct(ctx, productID, quantity)
	case -2:
		return errors.New("库存不足")
	}

	// 成功：stockResult是新库存值
	logger.Debug("库存扣减成功", "product_id", productID, "quantity", quantity, "new_stock", stockResult)

	// 异步更新MySQL
	//  为什么不选择用stockResult去更新mysql呢？
	//  因为在高并发场景下，多个请求可能同时扣减库存，导致mysql库存更新不准确
	//  所以我们选择直接用变更量去更新mysql，并在mysql更新时加上保护条件，防止负库存
	go dao.asyncUpdateMySQLStockV2(ctx, productID, -quantity)

	// 延迟双删缓存
	// 为什么这样做?
	// 假设缓存和mysql都是100库存
	// 假设A扣减1个库存 扣减完是99 然后A删除商品缓存 此时商品信息缓存为空
	// 然后B查询商品信息 会从mysql加载库存100到缓存
	// 然后A更新mysql的请求才到  更新mysql库存99
	// 这样就会出现mysql库存99 缓存库存100的问题
	// 所以我们需要延迟再删除一次缓存 避免这种情况发生
	dao.ClearProductCache(ctx, productID)
	go func() {
		time.Sleep(100 * time.Millisecond)
		dao.ClearProductCache(ctx, productID)
	}()

	return nil
}

// safeInitStockAndDeduct 带分布式锁的安全预热与重试
func (dao *ProductDao) safeInitStockAndDeduct(ctx context.Context, productID int64, quantity int32) error {
	lockKey := fmt.Sprintf("lock:init:stock:%d", productID)

	// 获取分布式锁（10秒过期，防止死锁）
	// 这里锁的意义 防止多人从mysql里加载 然后扣减导致超卖
	// setNX 只有该键不存在的时候才能被设置
	acquired, err := dao.redis.SetNX(ctx, lockKey, 1, 10*time.Second).Result()
	if err != nil || !acquired {
		return errors.New("系统繁忙，请重试")
	}
	defer dao.redis.Del(ctx, lockKey) // 确保释放锁

	// 双重检查（DCL模式）
	// 万一当我拿到锁的时候 别人就已经加载好了
	redisKey := getProductStockKey(productID)
	if exists, _ := dao.redis.Exists(ctx, redisKey).Result(); exists == 0 {
		if err := dao.initStockFromMySQL(ctx, productID); err != nil {
			logger.Error("库存预热失败", "product_id", productID, "err", err)
			return fmt.Errorf("系统初始化中: %w", err)
		}
		logger.Info("库存预热成功", "product_id", productID)
	}

	// 重试扣减
	return dao.DeductStock(ctx, productID, quantity)
}

// initStockFromMySQL 从MySQL加载库存（不adjust）
func (dao *ProductDao) initStockFromMySQL(ctx context.Context, productID int64) error {
	var product model.Product
	if err := dao.db.WithContext(ctx).First(&product, productID).Error; err != nil {
		return err
	}
	redisKey := getProductStockKey(productID)
	return dao.redis.Set(ctx, redisKey, product.Stock, 0).Err()
}

// asyncUpdateMySQLStockV2 增强版异步更新 - 带保护条件和告警
func (dao *ProductDao) asyncUpdateMySQLStockV2(ctx context.Context, productID int64, change int32) {
	for i := 0; i < 3; i++ {
		// 确保库存不为负数
		result := dao.db.WithContext(ctx).
			Model(&model.Product{}).
			Where("id = ? AND stock + ? >= 0", productID, change).
			Update("stock", gorm.Expr("stock + ?", change))

		if result.Error == nil && result.RowsAffected > 0 {
			logger.Debug("MySQL库存同步成功", "product_id", productID, "change", change)
			return
		}

		if result.Error != nil {
			logger.Error("MySQL库存更新失败", "product_id", productID, "change", change, "err", result.Error)
		} else if result.RowsAffected == 0 { // 执行成功 但是没更新 即stock+change<0
			// WHERE条件不满足，说明数据不一致
			logger.Warn("MySQL库存更新条件不满足", "product_id", productID, "change", change)
		}

		time.Sleep(time.Duration(i+1) * time.Second)
	}

	// 优化点：记录CRITICAL日志，触发人工补偿
	logger.Error("MySQL库存同步失败，需要人工补偿",
		"product_id", productID,
		"change", change,
		"alarm", "CRITICAL")
	// TODO: 发送到Kafka/钉钉等告警系统
}

// ReturnStock 归还库存（Redis优化版）- Lua返回状态码
func (dao *ProductDao) ReturnStock(ctx context.Context, productID int64, quantity int32) error {
	if quantity <= 0 {
		return errors.New("归还数量必须大于0")
	}

	redisKey := getProductStockKey(productID)

	luaScript := `
        local stock = redis.call('get', KEYS[1])
        if not stock then
            return -1  -- 键不存在
        end
        
        local stockNum = tonumber(stock)
        local quantity = tonumber(ARGV[1])
        local newStock = stockNum + quantity
        
        -- 优化点：上限保护（防止库存膨胀攻击）
        if newStock > 1000000 then
            return -2  -- 超过上限
        end
        
        redis.call('incrby', KEYS[1], quantity)
        return newStock  -- 成功，返回新库存值
    `

	result, err := dao.redis.Eval(ctx, luaScript, []string{redisKey}, quantity).Result()
	if err != nil {
		return fmt.Errorf("redis执行失败: %w", err)
	}

	returnValue := result.(int64)
	switch returnValue {
	case -1:
		// 键不存在，从MySQL加载并归还
		logger.Info("归还时库存键不存在，尝试加载", "product_id", productID)
		return dao.initStockFromMySQLAndReturn(ctx, productID, quantity)
	case -2:
		return errors.New("库存超过上限，异常")
	}

	logger.Debug("库存归还成功", "product_id", productID, "new_stock", returnValue)

	go dao.asyncUpdateMySQLStockV2(ctx, productID, quantity)
	dao.ClearProductCache(ctx, productID)

	return nil
}

// initStockFromMySQLAndReturn ReturnStock专用：加载库存并重新执行归还
func (dao *ProductDao) initStockFromMySQLAndReturn(ctx context.Context, productID int64, quantity int32) error {
	var product model.Product
	if err := dao.db.WithContext(ctx).First(&product, productID).Error; err != nil {
		return err
	}

	redisKey := getProductStockKey(productID)
	// 加载库存后，重新执行归还（adjust场景）
	if err := dao.redis.Set(ctx, redisKey, product.Stock, 0).Err(); err != nil {
		return err
	}

	logger.Info("库存加载成功", "product_id", productID, "initial_stock", product.Stock)
	return dao.ReturnStock(ctx, productID, quantity) // 重试归还
}
