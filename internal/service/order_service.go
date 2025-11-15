package service

import (
	"context"
	"errors"
	"time"

	"github.com/CCDD2022/seckill-system/internal/dao"
	"github.com/CCDD2022/seckill-system/internal/model"
	"github.com/CCDD2022/seckill-system/pkg/e"
	"github.com/CCDD2022/seckill-system/proto_output/order"
	"gorm.io/gorm"
)

type OrderService struct {
	orderDao *dao.OrderDao
	order.UnimplementedOrderServiceServer
}

func NewOrderService(orderDao *dao.OrderDao) *OrderService {
	return &OrderService{
		orderDao: orderDao,
	}
}

// CreateOrder 创建订单
func (s *OrderService) CreateOrder(ctx context.Context, req *order.CreateOrderRequest) (*order.CreateOrderResponse, error) {
	// 创建订单模型
	newOrder := &model.Order{
		UserID:     req.UserId,
		ProductID:  req.ProductId,
		Quantity:   req.Quantity,
		TotalPrice: req.TotalPrice,
		Status:     model.OrderStatusPending,
	}

	// 保存到数据库
	err := s.orderDao.CreateOrder(ctx, newOrder)
	if err != nil {
		return &order.CreateOrderResponse{
			Code:    e.ERROR,
			Message: e.GetMsg(e.ERROR),
		}, err
	}

	return &order.CreateOrderResponse{
		Code:    e.SUCCESS,
		Message: e.GetMsg(e.SUCCESS),
		OrderId: newOrder.ID,
	}, nil
}

// GetOrder 获取订单详情
func (s *OrderService) GetOrder(ctx context.Context, req *order.GetOrderRequest) (*order.GetOrderResponse, error) {
	orderData, err := s.orderDao.GetOrderByID(ctx, req.OrderId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &order.GetOrderResponse{
				Code:    e.ERROR_NOT_EXIST,
				Message: "订单不存在",
			}, nil
		}
		return &order.GetOrderResponse{
			Code:    e.ERROR,
			Message: e.GetMsg(e.ERROR),
		}, err
	}

	orderProto := &order.Order{
		Id:         orderData.ID,
		UserId:     orderData.UserID,
		ProductId:  orderData.ProductID,
		Quantity:   orderData.Quantity,
		TotalPrice: orderData.TotalPrice,
		Status:     orderData.Status,
		CreatedAt:  orderData.CreatedAt.Format(time.RFC3339),
		UpdatedAt:  orderData.UpdatedAt.Format(time.RFC3339),
	}

	return &order.GetOrderResponse{
		Code:    e.SUCCESS,
		Message: e.GetMsg(e.SUCCESS),
		Order:   orderProto,
	}, nil
}

// ListUserOrders 获取用户订单列表
func (s *OrderService) ListUserOrders(ctx context.Context, req *order.ListUserOrdersRequest) (*order.ListUserOrdersResponse, error) {
	orders, total, err := s.orderDao.GetUserOrders(ctx, req.UserId, req.Page, req.PageSize)
	if err != nil {
		return &order.ListUserOrdersResponse{
			Code:    e.ERROR,
			Message: e.GetMsg(e.ERROR),
		}, err
	}

	var orderList []*order.Order
	for _, o := range orders {
		orderList = append(orderList, &order.Order{
			Id:         o.ID,
			UserId:     o.UserID,
			ProductId:  o.ProductID,
			Quantity:   o.Quantity,
			TotalPrice: o.TotalPrice,
			Status:     o.Status,
			CreatedAt:  o.CreatedAt.Format(time.RFC3339),
			UpdatedAt:  o.UpdatedAt.Format(time.RFC3339),
		})
	}

	return &order.ListUserOrdersResponse{
		Code:    e.SUCCESS,
		Message: e.GetMsg(e.SUCCESS),
		Orders:  orderList,
		Total:   int32(total),
	}, nil
}

// CancelOrder 取消订单
func (s *OrderService) CancelOrder(ctx context.Context, req *order.CancelOrderRequest) (*order.CancelOrderResponse, error) {
	err := s.orderDao.CancelOrder(ctx, req.OrderId, req.UserId)
	if err != nil {
		return &order.CancelOrderResponse{
			Code:    e.ERROR,
			Message: "取消订单失败",
		}, err
	}

	return &order.CancelOrderResponse{
		Code:    e.SUCCESS,
		Message: "订单已取消",
	}, nil
}
