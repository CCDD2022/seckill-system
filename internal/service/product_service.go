package service

import (
	"context"
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
		CreatedAt:   productInfo.CreatedAt.Unix(),
		UpdatedAt:   productInfo.UpdatedAt.Unix(),
	}

	if productInfo.SeckillStartTime != nil {
		productRes.SeckillStartTime = productInfo.SeckillStartTime.Unix()
	}
	if productInfo.SeckillEndTime != nil {
		productRes.SeckillEndTime = productInfo.SeckillEndTime.Unix()
	}

	return &product.GetProductResponse{
		Code:    e.SUCCESS,
		Message: e.GetMsg(e.SUCCESS),
		Product: productRes,
	}, nil
}

// CreateProduct 创建商品
func (s *ProductService) CreateProduct(ctx context.Context, request *product.CreateProductRequest) (*product.CreateProductResponse, error) {
	var startTimePtr, endTimePtr *time.Time
	if request.SeckillStartTime > 0 {
		st := time.Unix(request.SeckillStartTime, 0)
		startTimePtr = &st
	}
	if request.SeckillEndTime > 0 {
		et := time.Unix(request.SeckillEndTime, 0)
		endTimePtr = &et
	}
	productModel := &model.Product{
		Name:             request.Name,
		Description:      request.Description,
		Price:            request.Price,
		Stock:            request.Stock,
		ImageURL:         request.ImageUrl,
		SeckillStartTime: startTimePtr,
		SeckillEndTime:   endTimePtr,
	}

	// 创建商品
	id, err := s.productDao.CreateProduct(ctx, productModel)
	if err != nil {
		return &product.CreateProductResponse{
			Code:    e.ERROR,
			Message: e.GetMsg(e.ERROR),
		}, err
	}

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

	var startTime, endTime *time.Time
	if request.SeckillStartTime > 0 {
		st := time.Unix(request.SeckillStartTime, 0)
		startTime = &st
	}
	if request.SeckillEndTime > 0 {
		et := time.Unix(request.SeckillEndTime, 0)
		endTime = &et
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
	if startTime != nil {
		updates["seckill_start_time"] = *startTime
	}
	if endTime != nil {
		updates["seckill_end_time"] = *endTime
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
		}, nil
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

	return &product.DeleteProductResponse{
		Code:    e.SUCCESS,
		Message: e.GetMsg(e.SUCCESS),
	}, nil
}

// ListProducts 分页查询商品列表（带缓存和业务逻辑）
func (s *ProductService) ListProducts(ctx context.Context, request *product.ListProductsRequest) (*product.ListProductsResponse, error) {
	// 计算偏移量
	offset := (request.Page - 1) * request.PageSize
	// 直接从数据库读取，支持状态筛选（-1 全部）
	products, total, err := s.productDao.ListProductsFromDBWithStatus(ctx, offset, request.PageSize, request.Status)
	if err != nil {
		return &product.ListProductsResponse{Code: e.ERROR, Message: e.GetMsg(e.ERROR)}, err
	}
	if int64(offset) >= total {
		return s.buildListResponse([]*model.Product{}, total, e.SUCCESS), nil
	}
	return s.buildListResponse(products, total, e.SUCCESS), nil
}

// buildListResponse 构建列表响应
func (s *ProductService) buildListResponse(products []*model.Product, total int64, code int) *product.ListProductsResponse {
	var productList []*product.Product
	for _, p := range products {
		item := &product.Product{
			Id:          p.ID,
			Name:        p.Name,
			Description: p.Description,
			Price:       p.Price,
			Stock:       p.Stock,
			ImageUrl:    p.ImageURL,
			CreatedAt:   p.CreatedAt.Unix(),
			UpdatedAt:   p.UpdatedAt.Unix(),
		}
		if p.SeckillStartTime != nil {
			item.SeckillStartTime = p.SeckillStartTime.Unix()
		}
		if p.SeckillEndTime != nil {
			item.SeckillEndTime = p.SeckillEndTime.Unix()
		}
		productList = append(productList, item)
	}

	return &product.ListProductsResponse{
		Code:     int32(code),
		Message:  e.GetMsg(code),
		Products: productList,
		Total:    int32(total),
	}
}
