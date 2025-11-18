package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/CCDD2022/seckill-system/pkg/logger"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

// loginResponse matches auth.LoginResponse relevant fields
type loginResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Token   string `json:"token"`
}

// seckillResp captures success flag for post-run analysis
type seckillResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Success bool   `json:"success"`
	OrderID int64  `json:"order_id"`
}

func main() {
	var (
		gateway     = flag.String("gateway", "http://localhost:8080", "API Gateway base URL")
		productID   = flag.Int64("product", 1, "Product ID for seckill")
		quantity    = flag.Int("quantity", 1, "Purchase quantity")
		users       = flag.Int("users", 50, "Number of virtual users (tokens) to prepare")
		rate        = flag.Int("rate", 200, "Requests per second")
		duration    = flag.String("duration", "30s", "Attack duration (e.g. 10s, 1m")
		password    = flag.String("password", "password", "Password used for all test users")
		register    = flag.Bool("register", false, "Register users before login (if they might not exist)")
		productList = flag.String("productList", "", "Comma separated product IDs (optional: random pick per request)")
		outJSON     = flag.String("out", "vegeta_results.json", "Summary JSON output file")
	)
	flag.Parse()

	attackDuration, err := time.ParseDuration(*duration)
	if err != nil {
		logger.Fatal("invalid duration", "err", err)
	}

	// Prepare users
	tokens := prepareTokens(*gateway, *users, *password, *register)
	if len(tokens) == 0 {
		logger.Fatal("no tokens prepared; aborting")
	}

	// Parse optional product list
	var productIDs []int64
	if *productList != "" {
		for _, part := range bytes.Split([]byte(*productList), []byte(",")) {
			var id int64
			_, err := fmt.Sscanf(string(bytes.TrimSpace(part)), "%d", &id)
			if err == nil && id > 0 {
				productIDs = append(productIDs, id)
			}
		}
	}
	rand.Seed(time.Now().UnixNano())

	// Custom targeter cycling through tokens
	var counter uint64
	targeter := func(t *vegeta.Target) error {
		idx := atomic.AddUint64(&counter, 1) - 1
		token := tokens[idx%uint64(len(tokens))]
		pid := *productID
		if len(productIDs) > 0 { // random product id if list provided
			pid = productIDs[rand.Intn(len(productIDs))]
		}
		bodyMap := map[string]any{
			"product_id": pid,
			"quantity":   *quantity,
		}
		b, _ := json.Marshal(bodyMap)
		t.Method = http.MethodPost
		t.URL = fmt.Sprintf("%s/api/v1/seckill/execute", *gateway)
		t.Body = b
		t.Header = http.Header{}
		t.Header.Set("Content-Type", "application/json")
		t.Header.Set("Authorization", "Bearer "+token)
		return nil
	}

	attacker := vegeta.NewAttacker()
	var metrics vegeta.Metrics
	successLogical := uint64(0)
	totalLogical := uint64(0)

	for res := range attacker.Attack(targeter, vegeta.Rate{Freq: *rate, Per: time.Second}, attackDuration, "seckill_test") {
		metrics.Add(res)
		atomic.AddUint64(&totalLogical, 1)
		// Check JSON success field
		var sr seckillResp
		if err := json.Unmarshal(res.Body, &sr); err == nil {
			if sr.Success {
				atomic.AddUint64(&successLogical, 1)
			}
		}
	}
	metrics.Close()

	logicalSuccessRatio := float64(successLogical) / float64(max(1, totalLogical))

	summary := map[string]any{
		"attack": map[string]any{
			"rate_rps": *rate,
			"duration": attackDuration.String(),
			"users":    *users,
		},
		"vegeta_metrics": map[string]any{
			"requests":           metrics.Requests,
			"rate":               metrics.Rate,
			"throughput":         metrics.Throughput,
			"success_ratio_http": metrics.Success,
			"latency_mean_ms":    metrics.Latencies.Mean.Seconds() * 1000,
			"latency_p95_ms":     metrics.Latencies.P95.Seconds() * 1000,
			"latency_p99_ms":     metrics.Latencies.P99.Seconds() * 1000,
			"errors":             metrics.Errors,
		},
		"logical_success_ratio": logicalSuccessRatio,
		"logical_success":       successLogical,
		"logical_total":         totalLogical,
		"timestamp":             time.Now().Format(time.RFC3339),
	}

	data, _ := json.MarshalIndent(summary, "", "  ")
	if err := os.WriteFile(*outJSON, data, 0644); err != nil {
		logger.Warn("write summary failed", "err", err)
	}
	fmt.Println(string(data))
}

func prepareTokens(gateway string, users int, password string, doRegister bool) []string {
	tokens := make([]string, 0, users)
	client := &http.Client{Timeout: 5 * time.Second}
	for i := 0; i < users; i++ {
		uname := fmt.Sprintf("lt_user_%d", i)
		if doRegister {
			regBody := map[string]any{
				"username": uname,
				"password": password,
				"email":    fmt.Sprintf("%s@example.com", uname),
				"phone":    fmt.Sprintf("1380014%04d", i),
			}
			if err := postJSON(client, fmt.Sprintf("%s/api/v1/auth/register", gateway), regBody, nil); err != nil {
				logger.Warn("register failed", "username", uname, "err", err)
			}
		}
		var lr loginResponse
		loginBody := map[string]string{"username": uname, "password": password}
		err := postJSON(client, fmt.Sprintf("%s/api/v1/auth/login", gateway), loginBody, &lr)
		if err != nil || lr.Token == "" {
			logger.Warn("login failed", "username", uname, "err", err)
			continue
		}
		tokens = append(tokens, lr.Token)
	}
	return tokens
}

func postJSON(client *http.Client, url string, payload any, out any) error {
	b, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("status %d body %s", resp.StatusCode, string(body))
	}
	if out != nil {
		_ = json.Unmarshal(body, out)
	}
	return nil
}

func max(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}
