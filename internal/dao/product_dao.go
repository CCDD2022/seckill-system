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
	redis redis.UniversalClient
}

func NewProductDao(db *gorm.DB, redis redis.UniversalClient) *ProductDao {
	return &ProductDao{
		db:    db,
		redis: redis,
	}
}

// 缓存相关常量
const (
	productStockKeyTemplate = "stock:%d"
	productCacheKeyTemplate = "product:%d"
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
	err := dao.db.WithContext(ctx).Create(product).Error
	if err != nil {
		return 0, err
	}
	return product.ID, nil
}

// DeleteProductByID 删除商品
func (dao *ProductDao) DeleteProductByID(ctx context.Context, id int64) error {
	dao.ClearProductCache(ctx, id)
	return dao.db.WithContext(ctx).Delete(&model.Product{}, id).Error
}

// UpdateProduct 更新商品
func (dao *ProductDao) UpdateProduct(ctx context.Context, id int64, updates map[string]interface{}) error {
	dao.ClearProductCache(ctx, id)
	return dao.db.WithContext(ctx).Model(&model.Product{}).Where("id = ?", id).Updates(updates).Error
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
	fmt.Println(now)

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

	// SAdd 向集合中添加成员

	// 标记该商品库存已变更，交由对账批处理服务合并更新MySQL
	_ = dao.redis.SAdd(ctx, productDirtySetKey, strconv.FormatInt(productID, 10)).Err()

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
	acquired, err := dao.redis.SetNX(ctx, lockKey, 1, 30*time.Second).Result()
	if err != nil {
		return errors.New("系统繁忙")
	}

	if !acquired {
		// 未获取到锁，说明已有线程在加载，等待一段时间后重试扣减
		time.Sleep(200 * time.Millisecond)
		return dao.DeductStock(ctx, productID, quantity)
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

	// 标记该商品库存已变更，交由对账批处理服务合并更新MySQL
	_ = dao.redis.SAdd(ctx, productDirtySetKey, strconv.FormatInt(productID, 10)).Err()
	// 延迟双删，保持与扣减路径一致，降低脏读概率
	dao.ClearProductCache(ctx, productID)
	go func() {
		time.Sleep(100 * time.Millisecond)
		dao.ClearProductCache(ctx, productID)
	}()

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
