package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/CCDD2022/seckill-system/pkg/e"
	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// IPRateLimiter IP限流器
type IPRateLimiter struct {
	ips map[string]*rate.Limiter
	mu  *sync.RWMutex
	r   rate.Limit // 每秒允许的请求数
	b   int        // 令牌桶容量
}

// NewIPRateLimiter 创建IP限流器
func NewIPRateLimiter(r rate.Limit, b int) *IPRateLimiter {
	return &IPRateLimiter{
		ips: make(map[string]*rate.Limiter),
		mu:  &sync.RWMutex{},
		r:   r,
		b:   b,
	}
}

// AddIP 为IP创建限流器
func (i *IPRateLimiter) AddIP(ip string) *rate.Limiter {
	i.mu.Lock()
	defer i.mu.Unlock()

	limiter := rate.NewLimiter(i.r, i.b)
	i.ips[ip] = limiter

	return limiter
}

// GetLimiter 获取IP对应的限流器
func (i *IPRateLimiter) GetLimiter(ip string) *rate.Limiter {
	i.mu.Lock()
	limiter, exists := i.ips[ip]

	if !exists {
		i.mu.Unlock()
		return i.AddIP(ip)
	}

	i.mu.Unlock()
	return limiter
}

// RateLimitMiddleware 全局限流中间件
// r: 每秒允许的请求数
// b: 令牌桶容量（允许的突发流量）
func RateLimitMiddleware(r rate.Limit, b int) gin.HandlerFunc {
	limiter := NewIPRateLimiter(r, b)

	return func(c *gin.Context) {
		ip := c.ClientIP()
		limiterForIP := limiter.GetLimiter(ip)

		if !limiterForIP.Allow() {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"code":    e.ERROR,
				"message": "请求过于频繁，请稍后再试",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// SeckillRateLimitMiddleware 秒杀专用限流中间件（更严格）
func SeckillRateLimitMiddleware() gin.HandlerFunc {
	// 每个IP每秒最多5个请求，允许10个突发请求
	return RateLimitMiddleware(5, 10)
}

// CleanupIPRateLimiter 定期清理不活跃的IP限流器
func (i *IPRateLimiter) CleanupStaleIPs(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		i.mu.Lock()
		// 清理超过1小时没有活动的IP
		for ip := range i.ips {
			delete(i.ips, ip)
		}
		i.mu.Unlock()
	}
}
