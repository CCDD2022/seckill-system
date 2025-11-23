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
	"github.com/CCDD2022/seckill-system/internal/mq"
	"github.com/CCDD2022/seckill-system/pkg/e"
	"github.com/CCDD2022/seckill-system/pkg/logger"
	"github.com/CCDD2022/seckill-system/proto_output/order"
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
	mqPool   *mq.Pool
	order.UnimplementedOrderServiceServer
}

// NewOrderServiceWithMQ 带MQ发布能力的订单服务（使用生产者池）
func NewOrderServiceWithMQ(orderDao *dao.OrderDao, mqPool *mq.Pool) *OrderService {
	return &OrderService{
		orderDao: orderDao,
		mqPool:   mqPool,
	}
}

// CreateOrder 创建订单
func (s *OrderService) CreateOrder(ctx context.Context, req *order.CreateOrderRequest) (*order.CreateOrderResponse, error) {
	// 创建订单模型  待支付
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
	// 读取订单用于校验与事件载荷
	ord, getErr := s.orderDao.GetOrderByID(ctx, req.OrderId)
	if getErr != nil {
		if errors.Is(getErr, gorm.ErrRecordNotFound) {
			return &order.CancelOrderResponse{Code: e.ERROR_NOT_EXIST, Message: "订单不存在"}, nil
		}
		return &order.CancelOrderResponse{Code: e.ERROR, Message: "查询订单失败"}, getErr
	}

	// 订单ID和执行人ID校验
	if ord.UserID != req.UserId {
		return &order.CancelOrderResponse{Code: e.ERROR, Message: "无权取消该订单"}, nil
	}

	// 仅允许待支付订单取消
	if ord.Status != model.OrderStatusPending {
		// 非待支付状态不执行取消与回补（幂等/避免多次回补）
		return &order.CancelOrderResponse{Code: e.SUCCESS, Message: "订单状态不可取消或已处理"}, nil
	}

	// 状态改为Canceled
	err := s.orderDao.CancelOrder(ctx, req.OrderId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &order.CancelOrderResponse{
				Code:    e.ERROR_ORDER_STATUS_CHANGED,
				Message: e.GetMsg(e.ERROR_ORDER_STATUS_CHANGED),
			}, nil
		}
		return &order.CancelOrderResponse{
			Code:    e.ERROR,
			Message: "取消订单失败",
		}, err
	}

	// 发布取消事件（生产者池异步发布，不等待确认）
	if s.mqPool != nil {
		// 使用确定性幂等ID（不包含时间）避免重复取消产生不同事件ID
		evt := orderCanceledEvent{
			EventID:    deterministicEventID(req.OrderId, ord.ProductID, req.UserId, "cancel"),
			OccurredAt: time.Now().Unix(),
			OrderID:    req.OrderId,
			UserID:     req.UserId,
			ProductID:  ord.ProductID,
			Quantity:   ord.Quantity,
		}
		if b, mErr := json.Marshal(evt); mErr == nil {
			// 使用事件ID作为 AMQP MessageId，实现跨队列幂等追踪
			if err := s.mqPool.PublishAsyncWithID("seckill.exchange", orderCanceledKey, b, evt.EventID); err != nil {
				logger.Warn("订单取消事件发布失败", "order_id", req.OrderId, "err", err)
			} else {
				logger.Info("订单取消事件已发布", "order_id", req.OrderId, "product_id", ord.ProductID, "qty", ord.Quantity, "event_id", evt.EventID)
			}
		} else {
			logger.Warn("订单取消事件序列化失败", "order_id", req.OrderId, "err", mErr)
		}
	}

	return &order.CancelOrderResponse{
		Code:    e.SUCCESS,
		Message: "订单已取消",
	}, nil
}

// PayOrder 支付订单（模拟）
func (s *OrderService) PayOrder(ctx context.Context, req *order.PayOrderRequest) (*order.PayOrderResponse, error) {

	ord, err := s.orderDao.GetOrderByID(ctx, req.OrderId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &order.PayOrderResponse{Code: e.ERROR_NOT_EXIST, Message: "订单不存在"}, nil
		}
		return &order.PayOrderResponse{Code: e.ERROR, Message: "查询订单失败"}, err
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
		return &order.PayOrderResponse{Code: e.ERROR, Message: "支付失败"}, err
	}
	return &order.PayOrderResponse{Code: e.SUCCESS, Message: "支付成功"}, nil
}

// generateEventID 生成简易幂等事件ID（避免依赖外部库）
func deterministicEventID(orderID, productID, userID int64, action string) string {
	return fmt.Sprintf("%d-%d-%d-%s", orderID, productID, userID, action)
}
