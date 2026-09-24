package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CCDD2022/seckill-system/internal/dao"
	"github.com/CCDD2022/seckill-system/internal/model"
	"github.com/CCDD2022/seckill-system/pkg/e"
	"github.com/CCDD2022/seckill-system/proto_output/auth"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestConcurrentRegistrationReturnsConflictAgainstMySQL(t *testing.T) {
	if os.Getenv("MYSQL_INTEGRATION") != "1" {
		t.Skip("set MYSQL_INTEGRATION=1 against the local Compose MySQL")
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
	username := fmt.Sprintf("register_race_%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = db.Where("username = ?", username).Delete(&model.User{}).Error })
	service := NewAuthService(dao.NewAuthDao(db), "test-secret", 1)
	responses := make(chan int32, 2)
	errors := make(chan error, 2)
	var workers sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			resp, err := service.Register(context.Background(), &auth.RegisterRequest{
				Username: username, Password: "password123", Email: username + "@example.invalid",
			})
			if err != nil {
				errors <- err
				return
			}
			responses <- resp.GetCode()
		}()
	}
	close(start)
	workers.Wait()
	close(responses)
	close(errors)
	for err := range errors {
		t.Fatalf("registration returned a transport error: %v", err)
	}
	counts := map[int32]int{}
	for code := range responses {
		counts[code]++
	}
	if counts[e.SUCCESS] != 1 || counts[e.ERROR_USER_EXISTS] != 1 {
		t.Fatalf("registration codes = %v; want one success and one duplicate", counts)
	}
}
