// Package service 订单服务实现
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/CCDD2022/seckill-system/internal/dao"
	"github.com/CCDD2022/seckill-system/internal/model"
	"github.com/CCDD2022/seckill-system/pkg/e"
	"github.com/CCDD2022/seckill-system/proto_output/order"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

type orderCanceledEvent struct {
	EventID    string `json:"event_id"`
	OccurredAt int64  `json:"occurred_at"`
	OrderID    int64  `json:"order_id"`
	UserID     int64  `json:"user_id"`
	ProductID  int64  `json:"product_id"`
	Quantity   int32  `json:"quantity"`
}

const orderCanceledKey = "order.canceled"

type OrderService struct {
	orderDao *dao.OrderDao
	order.UnimplementedOrderServiceServer
}

func NewOrderService(orderDao *dao.OrderDao) *OrderService {
	return &OrderService{orderDao: orderDao}
}

// CreateOrder 创建订单
func (s *OrderService) CreateOrder(ctx context.Context, req *order.CreateOrderRequest) (*order.CreateOrderResponse, error) {
	// Only the reservation consumer may create an order. This old RPC bypassed
	// both stock reservation and the unique message identity.
	return nil, status.Error(codes.Unimplemented, "order creation is consumer-only")
}

// GetOrder 获取订单详情
func (s *OrderService) GetOrder(ctx context.Context, req *order.GetOrderRequest) (*order.GetOrderResponse, error) {
	if req == nil || req.OrderId <= 0 {
		return nil, status.Error(codes.InvalidArgument, "order ID must be positive")
	}
	if req.UserId <= 0 {
		return &order.GetOrderResponse{Code: e.ERROR_AUTH, Message: e.GetMsg(e.ERROR_AUTH)}, nil
	}
	orderData, err := s.orderDao.GetOrderByID(ctx, req.OrderId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &order.GetOrderResponse{
				Code:    e.ERROR_NOT_EXIST,
				Message: "订单不存在",
			}, nil
		}
		return nil, status.Error(codes.Unavailable, "order store unavailable")
	}
	if orderData.UserID != req.UserId {
		// 避免向其他用户透露该订单是否存在。
		return &order.GetOrderResponse{Code: e.ERROR_NOT_EXIST, Message: "订单不存在"}, nil
	}

	orderProto := &order.Order{
		Id:         orderData.ID,
		UserId:     orderData.UserID,
		ProductId:  orderData.ProductID,
		Quantity:   orderData.Quantity,
		TotalPrice: orderData.TotalPrice,
		Status:     orderData.Status,
		CreatedAt:  orderData.CreatedAt.Unix(),
		UpdatedAt:  orderData.UpdatedAt.Unix(),
	}

	return &order.GetOrderResponse{
		Code:    e.SUCCESS,
		Message: e.GetMsg(e.SUCCESS),
		Order:   orderProto,
	}, nil
}

// ListUserOrders 获取用户订单列表
func (s *OrderService) ListUserOrders(ctx context.Context, req *order.ListUserOrdersRequest) (*order.ListUserOrdersResponse, error) {
	if req == nil || req.UserId <= 0 || req.Page <= 0 || req.PageSize <= 0 || req.PageSize > 100 {
		return nil, status.Error(codes.InvalidArgument, "order query parameters are invalid")
	}
	orders, total, err := s.orderDao.GetUserOrders(ctx, req.UserId, req.Page, req.PageSize)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "order store unavailable")
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
			CreatedAt:  o.CreatedAt.Unix(),
			UpdatedAt:  o.UpdatedAt.Unix(),
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
	if req == nil || req.OrderId <= 0 || req.UserId <= 0 {
		return nil, status.Error(codes.InvalidArgument, "cancellation parameters are invalid")
	}
	// 读取订单用于校验与事件载荷
	ord, getErr := s.orderDao.GetOrderByID(ctx, req.OrderId)
	if getErr != nil {
		if errors.Is(getErr, gorm.ErrRecordNotFound) {
			return &order.CancelOrderResponse{Code: e.ERROR_NOT_EXIST, Message: "订单不存在"}, nil
		}
		return nil, status.Error(codes.Unavailable, "order store unavailable")
	}

	// 订单ID和执行人ID校验
	if ord.UserID != req.UserId {
		return &order.CancelOrderResponse{Code: e.ERROR, Message: "无权取消该订单"}, nil
	}

	// 仅允许待支付订单取消
	if ord.Status == model.OrderStatusCancelled {
		return &order.CancelOrderResponse{Code: e.SUCCESS, Message: "订单已取消"}, nil
	}
	if ord.Status != model.OrderStatusPending {
		return &order.CancelOrderResponse{Code: e.ERROR_ORDER_STATUS_CHANGED, Message: "订单状态不可取消"}, nil
	}

	// The state transition and its event must commit together. The relay
	// publishes from outbox_events, and the consumer restores stock once.
	evt := orderCanceledEvent{
		EventID:    deterministicEventID(req.OrderId, ord.ProductID, req.UserId, "cancel"),
		OccurredAt: time.Now().Unix(), OrderID: req.OrderId, UserID: req.UserId,
		ProductID: ord.ProductID, Quantity: ord.Quantity,
	}
	payload, err := json.Marshal(evt)
	if err != nil {
		return nil, status.Error(codes.Internal, "cancellation event generation failed")
	}
	err = s.orderDao.CancelOrderWithOutbox(ctx, req.OrderId, &model.OutboxEvent{
		EventID: evt.EventID, RoutingKey: orderCanceledKey, Payload: string(payload),
	})
	if err != nil {
		if errors.Is(err, dao.ErrOrderStatusChanged) {
			return &order.CancelOrderResponse{
				Code:    e.ERROR_ORDER_STATUS_CHANGED,
				Message: e.GetMsg(e.ERROR_ORDER_STATUS_CHANGED),
			}, nil
		}
		return nil, status.Error(codes.Unavailable, "order store unavailable")
	}

	return &order.CancelOrderResponse{
		Code:    e.SUCCESS,
		Message: "订单已取消，库存回补处理中",
	}, nil
}

// PayOrder 支付订单（模拟）
func (s *OrderService) PayOrder(ctx context.Context, req *order.PayOrderRequest) (*order.PayOrderResponse, error) {
	if req == nil || req.OrderId <= 0 || req.UserId <= 0 {
		return nil, status.Error(codes.InvalidArgument, "payment parameters are invalid")
	}

	ord, err := s.orderDao.GetOrderByID(ctx, req.OrderId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &order.PayOrderResponse{Code: e.ERROR_NOT_EXIST, Message: "订单不存在"}, nil
		}
		return nil, status.Error(codes.Unavailable, "order store unavailable")
	}
	if ord.UserID != req.UserId {
		return &order.PayOrderResponse{Code: e.ERROR, Message: "无权支付该订单"}, nil
	}
	if ord.Status != model.OrderStatusPending {
		return &order.PayOrderResponse{Code: e.ERROR_ORDER_STATUS_CHANGED, Message: e.GetMsg(e.ERROR_ORDER_STATUS_CHANGED)}, nil
	}

	if err := s.orderDao.PayOrder(ctx, req.OrderId); err != nil {
		if errors.Is(err, dao.ErrOrderStatusChanged) {
			return &order.PayOrderResponse{Code: e.ERROR_ORDER_STATUS_CHANGED, Message: e.GetMsg(e.ERROR_ORDER_STATUS_CHANGED)}, nil
		}
		return nil, status.Error(codes.Unavailable, "order store unavailable")
	}
	return &order.PayOrderResponse{Code: e.SUCCESS, Message: "支付成功"}, nil
}

// generateEventID 生成简易幂等事件ID（避免依赖外部库）
func deterministicEventID(orderID, productID, userID int64, action string) string {
	return fmt.Sprintf("%d-%d-%d-%s", orderID, productID, userID, action)
}
