// Command bench_http measures admission latency and final order throughput on
// a dedicated local Compose stack. It creates its own product and real MySQL
// users, each with a valid JWT, before starting the timed workload.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/CCDD2022/seckill-system/internal/dao"
	"github.com/CCDD2022/seckill-system/internal/dao/mysql"
	redisinit "github.com/CCDD2022/seckill-system/internal/dao/redis"
	"github.com/CCDD2022/seckill-system/internal/model"
	"github.com/CCDD2022/seckill-system/pkg/app"
	"github.com/CCDD2022/seckill-system/pkg/utils"
	"github.com/CCDD2022/seckill-system/proto_output/seckill"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type result struct {
	latency  time.Duration
	category string
}

type report struct {
	Mode                 string  `json:"mode"`
	Requests             int     `json:"requests"`
	Concurrency          int     `json:"concurrency"`
	InitialStock         int     `json:"initial_stock"`
	ProductID            int64   `json:"product_id"`
	Accepted             int     `json:"accepted"`
	BusinessRejected     int     `json:"business_rejected"`
	RateLimited          int     `json:"rate_limited"`
	OtherErrors          int     `json:"other_errors"`
	ResponseP50Ms        float64 `json:"response_p50_ms,omitempty"`
	ResponseP95Ms        float64 `json:"response_p95_ms,omitempty"`
	ResponseP99Ms        float64 `json:"response_p99_ms,omitempty"`
	AcceptedP99Ms        float64 `json:"accepted_p99_ms,omitempty"`
	AdmissionElapsedMs   float64 `json:"admission_elapsed_ms"`
	AdmissionRequestsPS  float64 `json:"admission_requests_per_sec"`
	AcceptedPS           float64 `json:"accepted_per_sec"`
	FinalOrders          int64   `json:"final_orders"`
	FinalElapsedMs       float64 `json:"final_elapsed_ms"`
	FinalOrdersPS        float64 `json:"final_orders_per_sec"`
	RedisStock           int64   `json:"redis_stock"`
	MySQLStock           int32   `json:"mysql_stock"`
	GoVisibleCPUs        int     `json:"go_visible_cpus"`
	FinalizationTimedOut bool    `json:"finalization_timed_out"`
}

func main() {
	requests := flag.Int("requests", 500, "number of distinct users and requests")
	concurrency := flag.Int("concurrency", 100, "simultaneous client workers")
	mode := flag.String("mode", "http", "http (through gateway) or grpc (direct internal service)")
	flag.Parse()
	if *requests <= 0 || *requests > 20000 || *concurrency <= 0 || *concurrency > *requests || (*mode != "http" && *mode != "grpc") {
		panic("expected --requests 1..20000, --concurrency 1..requests, --mode http|grpc")
	}

	cfg := app.BootstrapApp()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	db, err := mysql.InitDB(&cfg.Database.Mysql)
	must(err)
	rdb, err := redisinit.InitRedis(&cfg.Database.Redis)
	must(err)
	defer rdb.Close()

	startTime, endTime := time.Now().Add(-time.Minute), time.Now().Add(10*time.Minute)
	product := &model.Product{
		Name:             fmt.Sprintf("Benchmark %d", time.Now().UnixNano()),
		Price:            9.99,
		Stock:            int32(*requests),
		SeckillStartTime: &startTime,
		SeckillEndTime:   &endTime,
	}
	_, err = dao.NewProductDao(db, rdb).CreateProduct(ctx, product)
	must(err)

	baseUserID := time.Now().UnixNano() / 1000
	users := make([]model.User, *requests)
	for i := range users {
		users[i] = model.User{
			ID:           baseUserID + int64(i),
			Username:     fmt.Sprintf("bench_%d_%d", baseUserID, i),
			PasswordHash: "benchmark-only-no-login",
		}
	}
	must(db.WithContext(ctx).CreateInBatches(users, 200).Error)
	jwtUtil := utils.NewJWTUtil(cfg.JWT.Secret, cfg.JWT.ExpireHours)
	tokens := make([]string, len(users))
	for i, user := range users {
		tokens[i], err = jwtUtil.GenerateToken(user.ID, user.Username, false)
		must(err)
	}

	var httpClient *http.Client
	var grpcClient seckill.SeckillServiceClient
	if *mode == "http" {
		httpClient = &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
			MaxConnsPerHost: *concurrency, MaxIdleConns: *concurrency,
			MaxIdleConnsPerHost: *concurrency, IdleConnTimeout: 30 * time.Second,
		}}
	} else {
		connCtx, connCancel := context.WithTimeout(ctx, 10*time.Second)
		conn, dialErr := grpc.DialContext(connCtx, "seckill-service:50054",
			grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
		connCancel()
		must(dialErr)
		defer conn.Close()
		grpcClient = seckill.NewSeckillServiceClient(conn)
	}

	jobs := make(chan int, *requests)
	results := make(chan result, *requests)
	barrier := make(chan struct{})
	var workers sync.WaitGroup
	for i := 0; i < *concurrency; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-barrier
			for index := range jobs {
				if *mode == "http" {
					results <- callHTTP(httpClient, product.ID, tokens[index])
				} else {
					results <- callGRPC(grpcClient, product.ID, users[index].ID)
				}
			}
		}()
	}
	for i := range users {
		jobs <- i
	}
	close(jobs)
	started := time.Now()
	close(barrier)
	workers.Wait()
	admissionElapsed := time.Since(started)
	close(results)

	allLatencies := make([]time.Duration, 0, *requests)
	acceptedLatencies := make([]time.Duration, 0, *requests)
	r := report{Mode: *mode, Requests: *requests, Concurrency: *concurrency,
		InitialStock: *requests, ProductID: product.ID, GoVisibleCPUs: runtime.NumCPU()}
	for result := range results {
		allLatencies = append(allLatencies, result.latency)
		switch result.category {
		case "accepted":
			r.Accepted++
			acceptedLatencies = append(acceptedLatencies, result.latency)
		case "business_rejected":
			r.BusinessRejected++
		case "rate_limited":
			r.RateLimited++
		default:
			r.OtherErrors++
		}
	}
	r.ResponseP50Ms = percentile(allLatencies, 0.50)
	r.ResponseP95Ms = percentile(allLatencies, 0.95)
	r.ResponseP99Ms = percentile(allLatencies, 0.99)
	r.AcceptedP99Ms = percentile(acceptedLatencies, 0.99)
	r.AdmissionElapsedMs = ms(admissionElapsed)
	r.AdmissionRequestsPS = float64(*requests) / admissionElapsed.Seconds()
	r.AcceptedPS = float64(r.Accepted) / admissionElapsed.Seconds()

	finalDeadline := time.Now().Add(120 * time.Second)
	for {
		must(db.WithContext(ctx).Model(&model.Order{}).Where("product_id = ?", product.ID).Count(&r.FinalOrders).Error)
		if r.FinalOrders == int64(r.Accepted) || time.Now().After(finalDeadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	r.FinalizationTimedOut = r.FinalOrders != int64(r.Accepted)
	r.FinalElapsedMs = ms(time.Since(started))
	r.FinalOrdersPS = float64(r.FinalOrders) / time.Since(started).Seconds()
	r.RedisStock, err = rdb.Get(ctx, fmt.Sprintf("stock:%d", product.ID)).Int64()
	must(err)
	for i := 0; i < 100; i++ {
		must(db.WithContext(ctx).First(product, "id = ?", product.ID).Error)
		r.MySQLStock = product.Stock
		if int64(r.MySQLStock) == r.RedisStock {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	encoded, err := json.Marshal(r)
	must(err)
	fmt.Printf("BENCH_RESULT %s\n", encoded)
}

func callHTTP(client *http.Client, productID int64, token string) result {
	body := []byte(fmt.Sprintf(`{"product_id":%d,"quantity":1}`, productID))
	request, err := http.NewRequest(http.MethodPost, "http://api-gateway:8080/api/v1/seckill/execute", bytes.NewReader(body))
	if err != nil {
		return result{category: "error"}
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	started := time.Now()
	response, err := client.Do(request)
	if err != nil {
		return result{latency: time.Since(started), category: "error"}
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 8192))
	latency := time.Since(started)
	if err != nil {
		return result{latency: latency, category: "error"}
	}
	var parsed struct {
		State string `json:"state"`
		Code  string `json:"code"`
	}
	if json.Unmarshal(data, &parsed) != nil {
		return result{latency: latency, category: "error"}
	}
	switch {
	case response.StatusCode == http.StatusAccepted && parsed.State == "processing":
		return result{latency: latency, category: "accepted"}
	case response.StatusCode == http.StatusTooManyRequests:
		return result{latency: latency, category: "rate_limited"}
	case response.StatusCode == http.StatusConflict:
		return result{latency: latency, category: "business_rejected"}
	default:
		return result{latency: latency, category: "error"}
	}
}

func callGRPC(client seckill.SeckillServiceClient, productID, userID int64) result {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	started := time.Now()
	response, err := client.ExecuteSeckill(ctx, &seckill.SeckillRequest{
		UserId: userID, ProductId: productID, Quantity: 1,
	})
	latency := time.Since(started)
	if err != nil {
		switch status.Code(err) {
		case codes.AlreadyExists, codes.ResourceExhausted, codes.FailedPrecondition, codes.InvalidArgument, codes.NotFound:
			return result{latency: latency, category: "business_rejected"}
		}
		return result{latency: latency, category: "error"}
	}
	if response.GetSuccess() {
		return result{latency: latency, category: "accepted"}
	}
	return result{latency: latency, category: "business_rejected"}
}

func percentile(values []time.Duration, quantile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	index := int(math.Ceil(quantile*float64(len(values)))) - 1
	if index < 0 {
		index = 0
	}
	return ms(values[index])
}

func ms(duration time.Duration) float64 { return float64(duration) / float64(time.Millisecond) }

func must(err error) {
	if err != nil {
		panic(err)
	}
}
