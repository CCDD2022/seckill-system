package service

import (
	"context"
	"testing"
	"time"

	"github.com/CCDD2022/seckill-system/internal/dao"
	"github.com/CCDD2022/seckill-system/proto_output/product"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestProductStorageOutageIsUnavailableNotNotFound(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 50 * time.Millisecond, MaxRetries: 0})
	defer rdb.Close()
	service := NewProductService(dao.NewProductDao(nil, rdb))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response, err := service.GetProduct(ctx, &product.GetProductRequest{ProductId: 123})
	if response != nil || status.Code(err) != codes.Unavailable {
		t.Fatalf("response=%v err=%v; want gRPC Unavailable", response, err)
	}
}
