package service

import (
	"context"
	"log"
	"time"

	"github.com/CCDD2022/seckill-system/internal/dao"
	"github.com/CCDD2022/seckill-system/internal/model"
	"github.com/CCDD2022/seckill-system/pkg/e"
	"github.com/CCDD2022/seckill-system/proto_output/product"
)

type ProductService struct {
	productDao *dao.ProductDao
	product.UnimplementedProductServiceServer
}

func NewProductService(productDao *dao.ProductDao) *ProductService {
	return &ProductService{
		productDao: productDao,
	}
}

// GetProduct 获取商品详情
func (s *ProductService) GetProduct(ctx context.Context, request *product.GetProductRequest) (*product.GetProductResponse, error) {

	productInfo, err := s.productDao.GetProductByID(ctx, request.ProductId)
	if err != nil {
		// 商品不存在是业务错误，返回nil error
		return &product.GetProductResponse{
			Code:    e.ERROR_PRODUCT_NOT_EXISTS,
			Message: e.GetMsg(e.ERROR_PRODUCT_NOT_EXISTS),
		}, nil
	}

	productRes := &product.Product{
		Id:          productInfo.ID,
		Name:        productInfo.Name,
		Description: productInfo.Description,
		Price:       productInfo.Price,
		Stock:       productInfo.Stock,
		ImageUrl:    productInfo.ImageURL,
		CreatedAt:   productInfo.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   productInfo.UpdatedAt.Format(time.RFC3339),
	}

	return &product.GetProductResponse{
		Code:    e.SUCCESS,
		Message: e.GetMsg(e.SUCCESS),
		Product: productRes,
	}, nil
}

// CreateProduct 创建商品
func (s *ProductService) CreateProduct(ctx context.Context, request *product.CreateProductRequest) (*product.CreateProductResponse, error) {
	// 构建商品模型
	productModel := &model.Product{
		Name:        request.Name,
		Description: request.Description,
		Price:       request.Price,
		Stock:       request.Stock,
		ImageURL:    request.ImageUrl,
	}

	// 创建商品
	id, err := s.productDao.CreateProduct(ctx, productModel)
	if err != nil {
		return &product.CreateProductResponse{
			Code:    e.ERROR,
			Message: e.GetMsg(e.ERROR),
		}, err
	}

	// 清理列表缓存（异步，不阻塞主流程）
	go func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := s.productDao.ClearListCache(cleanupCtx); err != nil {
			log.Printf("清理列表缓存失败: %v", err)
		}
	}()

	return &product.CreateProductResponse{
		Code:      e.SUCCESS,
		Message:   e.GetMsg(e.SUCCESS),
		ProductId: id,
	}, nil
}

// UpdateProduct 更新商品
func (s *ProductService) UpdateProduct(ctx context.Context, request *product.UpdateProductRequest) (*product.UpdateProductResponse, error) {
	// 检查商品是否存在
	_, err := s.productDao.GetProductByID(ctx, request.ProductId)
	if err != nil {
		// 商品不存在是业务错误
		return &product.UpdateProductResponse{
			Code:    e.ERROR_PRODUCT_NOT_EXISTS,
			Message: e.GetMsg(e.ERROR_PRODUCT_NOT_EXISTS),
		}, nil
	}

	// 构建更新字段
	updates := make(map[string]interface{})
	if request.Name != "" {
		updates["name"] = request.Name
	}
	if request.Description != "" {
		updates["description"] = request.Description
	}
	if request.Price > 0 {
		updates["price"] = request.Price
	}
	if request.Stock >= 0 {
		updates["stock"] = request.Stock
	}
	if request.ImageUrl != "" {
		updates["image_url"] = request.ImageUrl
	}

	// 如果没有需要更新的字段，返回错误
	if len(updates) == 0 {
		return &product.UpdateProductResponse{
			Code:    e.INVALID_PARAMS,
			Message: e.GetMsg(e.INVALID_PARAMS),
		}, nil
	}

	// 更新商品
	err = s.productDao.UpdateProduct(ctx, request.ProductId, updates)
	if err != nil {
		// 数据库更新失败是系统错误
		return &product.UpdateProductResponse{
			Code:    e.ERROR,
			Message: e.GetMsg(e.ERROR),
		}, err
	}

	// 清理缓存（异步）
	go func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		s.productDao.ClearProductCache(cleanupCtx, request.ProductId)
		if err := s.productDao.ClearListCache(context.Background()); err != nil {
			log.Printf("清理列表缓存失败: %v", err)
		}
	}()

	return &product.UpdateProductResponse{
		Code:    e.SUCCESS,
		Message: e.GetMsg(e.SUCCESS),
	}, nil
}

// DeleteProduct 删除商品
func (s *ProductService) DeleteProduct(ctx context.Context, request *product.DeleteProductRequest) (*product.DeleteProductResponse, error) {
	// 检查商品是否存在
	_, err := s.productDao.GetProductByID(ctx, request.ProductId)
	if err != nil {
		// 商品不存在是业务错误
		return &product.DeleteProductResponse{
			Code:    e.ERROR_PRODUCT_NOT_EXISTS,
			Message: e.GetMsg(e.ERROR_PRODUCT_NOT_EXISTS),
		}, err
	}

	// 删除商品
	err = s.productDao.DeleteProductByID(ctx, request.ProductId)
	if err != nil {
		// 数据库删除失败是系统错误
		return &product.DeleteProductResponse{
			Code:    e.ERROR,
			Message: e.GetMsg(e.ERROR),
		}, err
	}

	// 清理缓存（异步）
	go func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		s.productDao.ClearProductCache(cleanupCtx, request.ProductId)
		if err := s.productDao.ClearListCache(cleanupCtx); err != nil {
			log.Printf("清理列表缓存失败: %v", err)
		}
	}()

	return &product.DeleteProductResponse{
		Code:    e.SUCCESS,
		Message: e.GetMsg(e.SUCCESS),
	}, nil
}

// ListProducts 分页查询商品列表（带缓存和业务逻辑）
func (s *ProductService) ListProducts(ctx context.Context, request *product.ListProductsRequest) (*product.ListProductsResponse, error) {
	// 计算偏移量
	offset := (request.Page - 1) * request.PageSize

	// 尝试从缓存获取
	cacheKey := dao.GetListCacheKey(request.Page, request.PageSize)
	cachedProducts, cachedTotal, err := s.productDao.GetProductsFromCache(ctx, cacheKey)
	if err == nil {
		// 缓存命中
		return s.buildListResponse(cachedProducts, cachedTotal, e.SUCCESS), nil
	}

	// 缓存未命中，查询数据库
	total, err := s.productDao.GetTotalProducts(ctx)
	if err != nil {
		return &product.ListProductsResponse{
			Code:    e.ERROR,
			Message: e.GetMsg(e.ERROR),
		}, err
	}

	// 检查是否超出数据范围（业务逻辑，返回nil error）
	if int64(offset) >= total {
		return s.buildListResponse([]*model.Product{}, total, e.ERROR_PRODUCT_NOT_EXISTS), nil
	}

	// 查询分页数据
	products, err := s.productDao.ListProductsFromDB(ctx, offset, request.PageSize)
	if err != nil {
		// 数据库查询失败是系统错误
		return &product.ListProductsResponse{
			Code:    e.ERROR,
			Message: e.GetMsg(e.ERROR),
		}, err
	}

	// 异步更新缓存
	go s.updateCache(context.Background(), cacheKey, products, total)

	return s.buildListResponse(products, total, e.SUCCESS), nil
}

// buildListResponse 构建列表响应
func (s *ProductService) buildListResponse(products []*model.Product, total int64, code int) *product.ListProductsResponse {
	var productList []*product.Product
	for _, p := range products {
		productList = append(productList, &product.Product{
			Id:          p.ID,
			Name:        p.Name,
			Description: p.Description,
			Price:       p.Price,
			Stock:       p.Stock,
			ImageUrl:    p.ImageURL,
			CreatedAt:   p.CreatedAt.Format(time.RFC3339),
			UpdatedAt:   p.UpdatedAt.Format(time.RFC3339),
		})
	}

	return &product.ListProductsResponse{
		Code:     int32(code),
		Message:  e.GetMsg(code),
		Products: productList,
		Total:    int32(total),
	}
}

// updateCache 异步更新缓存
func (s *ProductService) updateCache(ctx context.Context, cacheKey string, products []*model.Product, total int64) {
	type listCache struct {
		Products []*model.Product `json:"products"`
		Total    int64            `json:"total"`
	}
	response := listCache{Products: products, Total: total}

	if err := s.productDao.SetProductsToCache(ctx, cacheKey, response.Products, response.Total); err != nil {
		log.Printf("更新缓存失败: %v", err)
	}
}
