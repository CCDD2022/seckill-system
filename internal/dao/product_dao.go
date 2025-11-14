package dao

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/CCDD2022/seckill-system/pkg/logger"

	"github.com/CCDD2022/seckill-system/internal/model"
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
	productCacheKeyPrefix = "product:cache:id:"
	productListCacheKey   = "product:cache:list"
	cacheExpiration       = 30 * time.Minute
)

// getProductCacheKey 生成单个商品缓存键
func getProductCacheKey(id int64) string {
	return productCacheKeyPrefix + strconv.FormatInt(id, 10)
}

// GetListCacheKey 生成分页列表缓存键
func GetListCacheKey(page, pageSize int32) string {
	return fmt.Sprintf("%s:%d:%d", productListCacheKey, page, pageSize)
}

// GetProductByID 根据ID查询商品（带缓存）
func (dao *ProductDao) GetProductByID(ctx context.Context, id int64) (*model.Product, error) {
	cacheKey := getProductCacheKey(id)

	// 尝试从 Redis 获取
	cachedData, err := dao.redis.Get(ctx, cacheKey).Result()
	if errors.Is(err, redis.Nil) {
		// 缓存未命中，查询数据库
		var product model.Product
		err = dao.db.WithContext(ctx).First(&product, "id = ?", id).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 如果数据库也不存在  那么就缓存起来
			emptyProduct := &model.Product{} // ID默认为0
			cacheValue, _ := json.Marshal(emptyProduct)

			//设置短过期时间（5分钟），防止长期占用缓存
			if err := dao.redis.Set(ctx, cacheKey, cacheValue, 5*time.Minute).Err(); err != nil {
				logger.Info("缓存写入失败", "key", cacheKey, "err", err) // 至少日志记录
			}

			return nil, err // 返回原始错误给上游
		} else if err != nil {
			return nil, err // 其他DB错误不缓存（如连接超时）
		}

		// 写入缓存
		if productJSON, marshalErr := json.Marshal(product); marshalErr == nil {
			dao.redis.Set(ctx, cacheKey, productJSON, cacheExpiration)
		}

		return &product, nil
	} else if err != nil {
		return nil, err
	}

	// 缓存命中，反序列化
	var product model.Product
	if err := json.Unmarshal([]byte(cachedData), &product); err != nil {
		// 缓存数据意外损坏?  删除并且重新调用
		dao.redis.Del(ctx, cacheKey)
		return dao.GetProductByID(ctx, id)
	}

	if product.ID == 0 { // 0时我约定好的数据库中不存在 即无效请求
		return nil, gorm.ErrRecordNotFound
	}

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

// ListProductsFromDB 从数据库查询分页商品列表
func (dao *ProductDao) ListProductsFromDB(ctx context.Context, offset, limit int32) ([]*model.Product, error) {
	var products []*model.Product
	err := dao.db.WithContext(ctx).Offset(int(offset)).Limit(int(limit)).Find(&products).Error
	return products, err
}

// GetProductsFromCache 从缓存获取商品列表
func (dao *ProductDao) GetProductsFromCache(ctx context.Context, cacheKey string) ([]*model.Product, int64, error) {
	cachedData, err := dao.redis.Get(ctx, cacheKey).Result()
	if err != nil {
		return nil, 0, err
	}

	var cached struct {
		Products []*model.Product `json:"products"`
		Total    int64            `json:"total"`
	}
	if err := json.Unmarshal([]byte(cachedData), &cached); err != nil {
		return nil, 0, err
	}

	return cached.Products, cached.Total, nil
}

// SetProductsToCache 设置商品列表到缓存
func (dao *ProductDao) SetProductsToCache(ctx context.Context, cacheKey string, products []*model.Product, total int64) error {
	type listCache struct {
		Products []*model.Product `json:"products"`
		Total    int64            `json:"total"`
	}
	response := listCache{Products: products, Total: total}

	responseJSON, err := json.Marshal(response)
	if err != nil {
		return err
	}

	return dao.redis.Set(ctx, cacheKey, responseJSON, cacheExpiration).Err()
}

// ClearProductCache 清理商品缓存
func (dao *ProductDao) ClearProductCache(ctx context.Context, id int64) {
	cacheKey := getProductCacheKey(id)
	dao.redis.Del(ctx, cacheKey)
}

// ClearListCache 清理所有列表相关的缓存
func (dao *ProductDao) ClearListCache(ctx context.Context) error {
	iter := dao.redis.Scan(ctx, 0, productListCacheKey+":*", 0).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}

	if err := iter.Err(); err != nil {
		return err
	}

	if len(keys) > 0 {
		return dao.redis.Del(ctx, keys...).Err()
	}

	return nil
}
