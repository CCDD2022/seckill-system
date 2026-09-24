package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/CCDD2022/seckill-system/internal/dao"
	"github.com/CCDD2022/seckill-system/internal/model"
	"github.com/CCDD2022/seckill-system/pkg/e"
	"github.com/CCDD2022/seckill-system/proto_output/product"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
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
	if request == nil || request.ProductId <= 0 {
		return nil, status.Error(codes.InvalidArgument, "product ID must be positive")
	}
	productInfo, err := s.productDao.GetProductByID(ctx, request.ProductId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &product.GetProductResponse{Code: e.ERROR_PRODUCT_NOT_EXISTS, Message: e.GetMsg(e.ERROR_PRODUCT_NOT_EXISTS)}, nil
		}
		return nil, status.Error(codes.Unavailable, "product store unavailable")
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
	if request == nil || strings.TrimSpace(request.Name) == "" || request.Stock < 0 ||
		!model.ValidProductPrice(request.Price) ||
		request.SeckillStartTime < 0 || request.SeckillEndTime < 0 ||
		(request.SeckillStartTime == 0) != (request.SeckillEndTime == 0) ||
		(request.SeckillStartTime != 0 && request.SeckillStartTime >= request.SeckillEndTime) {
		return &product.CreateProductResponse{Code: e.INVALID_PARAMS, Message: e.GetMsg(e.INVALID_PARAMS)}, nil
	}
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
		if errors.Is(err, dao.ErrProductCreateUncertain) {
			base := status.New(codes.Unavailable, "product creation outcome unknown")
			withDetails, detailErr := base.WithDetails(&errdetails.ErrorInfo{
				Reason: "PRODUCT_CREATE_OUTCOME_UNKNOWN", Domain: "seckill-system",
				Metadata: map[string]string{"product_id": strconv.FormatInt(id, 10)},
			})
			if detailErr == nil {
				return nil, withDetails.Err()
			}
			return nil, base.Err()
		}
		return nil, status.Error(codes.Unavailable, "product store unavailable")
	}

	return &product.CreateProductResponse{
		Code:      e.SUCCESS,
		Message:   e.GetMsg(e.SUCCESS),
		ProductId: id,
	}, nil
}

// UpdateProduct 更新商品
func (s *ProductService) UpdateProduct(ctx context.Context, request *product.UpdateProductRequest) (*product.UpdateProductResponse, error) {
	if request == nil || request.ProductId <= 0 || (request.Price != 0 && !model.ValidProductPrice(request.Price)) {
		return &product.UpdateProductResponse{Code: e.INVALID_PARAMS, Message: e.GetMsg(e.INVALID_PARAMS)}, nil
	}
	// 检查商品是否存在
	existing, err := s.productDao.GetProductByID(ctx, request.ProductId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &product.UpdateProductResponse{Code: e.ERROR_PRODUCT_NOT_EXISTS, Message: e.GetMsg(e.ERROR_PRODUCT_NOT_EXISTS)}, nil
		}
		return nil, status.Error(codes.Unavailable, "product store unavailable")
	}

	if request.SeckillStartTime != 0 || request.SeckillEndTime != 0 ||
		(existing.SeckillStartTime != nil && request.Price > 0) {
		return nil, status.Error(codes.FailedPrecondition, "campaign time and price are immutable")
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
	if request.Stock != 0 {
		return &product.UpdateProductResponse{Code: e.INVALID_PARAMS, Message: "活动库存不可通过通用商品更新接口修改"}, nil
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
		if errors.Is(err, dao.ErrProductImmutable) {
			return nil, status.Error(codes.FailedPrecondition, "campaign data is immutable")
		}
		return nil, status.Error(codes.Unavailable, "product store unavailable")
	}

	return &product.UpdateProductResponse{
		Code:    e.SUCCESS,
		Message: e.GetMsg(e.SUCCESS),
	}, nil
}

// DeleteProduct 删除商品
func (s *ProductService) DeleteProduct(ctx context.Context, request *product.DeleteProductRequest) (*product.DeleteProductResponse, error) {
	if request == nil || request.ProductId <= 0 {
		return nil, status.Error(codes.InvalidArgument, "product ID must be positive")
	}
	// 检查商品是否存在
	_, err := s.productDao.GetProductByID(ctx, request.ProductId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &product.DeleteProductResponse{Code: e.ERROR_PRODUCT_NOT_EXISTS, Message: e.GetMsg(e.ERROR_PRODUCT_NOT_EXISTS)}, nil
		}
		return nil, status.Error(codes.Unavailable, "product store unavailable")
	}

	// 删除商品
	err = s.productDao.DeleteProductByID(ctx, request.ProductId)
	if err != nil {
		if errors.Is(err, dao.ErrProductInUse) {
			return nil, status.Error(codes.FailedPrecondition, "product is referenced by reservations or orders")
		}
		return nil, status.Error(codes.Unavailable, "product store unavailable")
	}

	return &product.DeleteProductResponse{
		Code:    e.SUCCESS,
		Message: e.GetMsg(e.SUCCESS),
	}, nil
}

// ListProducts 分页查询商品列表（带缓存和业务逻辑）
func (s *ProductService) ListProducts(ctx context.Context, request *product.ListProductsRequest) (*product.ListProductsResponse, error) {
	if request == nil || request.Page <= 0 || request.PageSize <= 0 || request.PageSize > 100 {
		return nil, status.Error(codes.InvalidArgument, "pagination is invalid")
	}
	// 计算偏移量
	offset := (request.Page - 1) * request.PageSize
	// 直接从数据库读取，支持状态筛选（-1 全部）
	products, total, err := s.productDao.ListProductsFromDBWithStatus(ctx, offset, request.PageSize, request.Status)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "product store unavailable")
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
