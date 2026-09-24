package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/CCDD2022/seckill-system/internal/dao"
	"github.com/CCDD2022/seckill-system/proto_output/seckill"
	"google.golang.org/grpc/codes"
	"gorm.io/gorm"
)

// SeckillService accepts a reservation only after Redis has atomically
// reserved inventory and persisted the order event to its stream. A separate
// relay delivers that event to RabbitMQ.
type SeckillService struct {
	productDao *dao.ProductDao
	seckill.UnimplementedSeckillServiceServer
}

func NewSeckillService(productDao *dao.ProductDao) *SeckillService {
	return &SeckillService{productDao: productDao}
}

type SeckillMessage struct {
	UserID     int64   `json:"user_id"`
	ProductID  int64   `json:"product_id"`
	Quantity   int32   `json:"quantity"`
	TotalPrice float64 `json:"total_price"`
}

func (s *SeckillService) ExecuteSeckill(ctx context.Context, req *seckill.SeckillRequest) (*seckill.SeckillResponse, error) {
	if req == nil || req.UserId <= 0 || req.ProductId <= 0 || req.Quantity <= 0 {
		return nil, seckillError(codes.InvalidArgument, "INVALID_REQUEST", "商品 ID 和购买数量必须大于零", "")
	}
	requestID := fmt.Sprintf("create:%d:%d", req.UserId, req.ProductId)

	campaign, err := s.productDao.GetCampaignSnapshot(ctx, req.ProductId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, seckillError(codes.NotFound, "PRODUCT_NOT_FOUND", "商品不存在", "")
		}
		if errors.Is(err, dao.ErrCampaignNotConfigured) {
			return nil, seckillError(codes.FailedPrecondition, "CAMPAIGN_NOT_CONFIGURED", "商品没有有效的秒杀活动", "")
		}
		return nil, seckillError(codes.Unavailable, "DEPENDENCY_UNAVAILABLE", "活动信息暂不可用", "")
	}
	if campaign.PriceCents <= 0 || campaign.PriceCents > math.MaxInt64/int64(req.Quantity) {
		return nil, seckillError(codes.Internal, "INVALID_CAMPAIGN_CONFIGURATION", "活动配置异常", "")
	}
	amountCents := campaign.PriceCents * int64(req.Quantity)
	msg := SeckillMessage{
		UserID: req.UserId, ProductID: req.ProductId, Quantity: req.Quantity,
		TotalPrice: float64(amountCents) / 100,
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return nil, seckillError(codes.Internal, "INTERNAL_ERROR", "暂时无法处理请求", "")
	}
	if err := s.productDao.ReserveStockAndEnqueue(ctx, req.ProductId, req.UserId, req.Quantity, campaign.PriceCents, requestID, body); err != nil {
		switch {
		case errors.Is(err, dao.ErrDuplicateReservation):
			return nil, seckillError(codes.AlreadyExists, "ALREADY_PARTICIPATED", "该商品已参与过秒杀", requestID)
		case errors.Is(err, dao.ErrSoldOut):
			return nil, seckillError(codes.ResourceExhausted, "SOLD_OUT", "库存不足", "")
		case errors.Is(err, dao.ErrCampaignNotActive):
			return nil, seckillError(codes.FailedPrecondition, "CAMPAIGN_INACTIVE", "当前不在秒杀活动时间内", "")
		case errors.Is(err, dao.ErrCampaignNotConfigured):
			return nil, seckillError(codes.FailedPrecondition, "CAMPAIGN_NOT_CONFIGURED", "商品没有有效的秒杀活动", "")
		case errors.Is(err, dao.ErrStockNotInitialized):
			return nil, seckillError(codes.Unavailable, "INVENTORY_UNAVAILABLE", "库存暂不可用", "")
		case errors.Is(err, dao.ErrEventPersistenceFailed):
			return nil, seckillError(codes.Unavailable, "EVENT_PERSISTENCE_FAILED", "订单事件暂无法保存，请稍后再试", "")
		case errors.Is(err, dao.ErrReservationUncertain):
			return nil, seckillError(codes.Unavailable, "OUTCOME_UNKNOWN", "请求结果尚未确认，请通过状态地址查询", requestID)
		default:
			return nil, seckillError(codes.Internal, "INTERNAL_ERROR", "暂时无法处理请求", "")
		}
	}
	return &seckill.SeckillResponse{
		Success: true,
		Message: "请求已受理，订单处理中",
	}, nil
}

func (s *SeckillService) GetReservationStatus(ctx context.Context, req *seckill.GetReservationStatusRequest) (*seckill.GetReservationStatusResponse, error) {
	if req == nil || req.UserId <= 0 || req.ProductId <= 0 {
		return nil, seckillError(codes.InvalidArgument, "INVALID_REQUEST", "用户 ID 和商品 ID 必须大于零", "")
	}
	snapshot, err := s.productDao.GetReservationSnapshot(ctx, req.UserId, req.ProductId)
	if err != nil {
		if errors.Is(err, dao.ErrReservationStatusNotFound) {
			return nil, seckillError(codes.NotFound, "RESERVATION_NOT_FOUND", "没有找到该商品的秒杀请求", "")
		}
		return nil, seckillError(codes.Unavailable, "STATUS_UNAVAILABLE", "暂时无法查询秒杀状态", "")
	}
	return &seckill.GetReservationStatusResponse{
		State: string(snapshot.State), OrderId: snapshot.OrderID, OrderStatus: snapshot.OrderStatus,
	}, nil
}
