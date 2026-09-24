package dao

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/CCDD2022/seckill-system/internal/model"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestProductCreationSeedsStockOrRollsBackAgainstMySQLAndRedis(t *testing.T) {
	if os.Getenv("MYSQL_INTEGRATION") != "1" {
		t.Skip("set MYSQL_INTEGRATION=1 against the local Compose MySQL and Redis")
	}
	password, err := os.ReadFile(os.Getenv("MYSQL_TEST_PASSWORD_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	dsn := fmt.Sprintf("seckill:%s@tcp(mysql:3306)/seckill_shop?charset=utf8mb4&parseTime=True&loc=UTC", strings.TrimSpace(string(password)))
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: "redis:6379"})
	defer rdb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	start, end := time.Now().Add(-time.Minute), time.Now().Add(time.Hour)
	product := &model.Product{
		Name:  fmt.Sprintf("product-integration-%d", time.Now().UnixNano()),
		Price: 9.99, Stock: 2, SeckillStartTime: &start, SeckillEndTime: &end,
	}
	productDao := NewProductDao(db, rdb)
	id, err := productDao.CreateProduct(ctx, product)
	if err != nil || id <= 0 {
		t.Fatalf("product creation failed: id=%d err=%v", id, err)
	}
	t.Cleanup(func() {
		_ = db.Delete(&model.Product{}, id).Error
		_ = rdb.Del(context.Background(), getProductStockKey(id), getProductCacheKey(id), getProductPriceKey(id), campaignKey(id)).Err()
	})
	var stored model.Product
	if err := db.First(&stored, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	stock, err := rdb.Get(ctx, getProductStockKey(id)).Int64()
	if err != nil || stock != 2 {
		t.Fatalf("Redis initial stock=%d err=%v; want 2", stock, err)
	}
	campaign, err := productDao.GetCampaignSnapshot(ctx, id)
	if err != nil || campaign.PriceCents != 999 || campaign.StartUnix != start.Unix() || campaign.EndUnix != end.Unix() {
		t.Fatalf("campaign cache not initialized: %+v err=%v", campaign, err)
	}
	active, err := rdb.HGet(ctx, campaignKey(id), "active").Result()
	if err != nil || active != "1" {
		t.Fatalf("created campaign was not activated: active=%q err=%v", active, err)
	}
	if err := productDao.UpdateProduct(ctx, id, map[string]any{"stock": 3}); err == nil {
		t.Fatal("generic product update unexpectedly changed campaign stock")
	}
	if err := productDao.DeleteProductByID(ctx, id); err == nil {
		t.Fatal("seckill product was deleted while needed for order audit")
	}

	// A Redis failure during creation must roll the MySQL row back.
	brokenRedis := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 100 * time.Millisecond, MaxRetries: 0})
	defer brokenRedis.Close()
	broken := &model.Product{Name: product.Name + "-rollback", Price: 9.99, Stock: 1}
	if _, err := NewProductDao(db, brokenRedis).CreateProduct(ctx, broken); err == nil {
		t.Fatal("creation succeeded while Redis was unavailable")
	}
	var count int64
	if err := db.Model(&model.Product{}).Where("name = ?", broken.Name).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("failed Redis seed left MySQL product: count=%d err=%v", count, err)
	}
}
