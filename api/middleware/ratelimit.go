package middleware

import (
	"net/http"
	"sync"

	"github.com/CCDD2022/seckill-system/config"
	"github.com/CCDD2022/seckill-system/pkg/e"
	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// IPRateLimiter IP限流器
type IPRateLimiter struct {
	ips map[string]*rate.Limiter
	mu  *sync.RWMutex
	r   rate.Limit // 每秒生成多少令牌
	b   int        // 令牌桶最多存多少令牌
}

// NewIPRateLimiter 为每一个IP创建一个限流器
func NewIPRateLimiter(r rate.Limit, b int) *IPRateLimiter {
	return &IPRateLimiter{
		ips: make(map[string]*rate.Limiter), // 为每个IP维护的独立的限流器
		mu:  &sync.RWMutex{},
		r:   r,
		b:   b,
	}
}

// GetLimiter 获取该IP的限流器  如果没有 那么就创建
func (i *IPRateLimiter) GetLimiter(ip string) *rate.Limiter {
	// 第一次检查：读锁（快速路径）
	i.mu.RLock()
	if limiter, exists := i.ips[ip]; exists {
		i.mu.RUnlock()
		return limiter
	}
	i.mu.RUnlock()

	// 第二次检查 + 写入：写锁（慢路径）
	i.mu.Lock()
	defer i.mu.Unlock()

	// 双重检查
	if limiter, exists := i.ips[ip]; exists {
		return limiter
	}

	limiter := rate.NewLimiter(i.r, i.b)
	i.ips[ip] = limiter
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

// GlobalRateLimit SeckillRateLimitMiddleware 秒杀专用限流中间件（更严格）
// Config-driven wrappers
func GlobalRateLimit(cfg *config.Config) gin.HandlerFunc {
	return RateLimitMiddleware(rate.Limit(cfg.RateLimits.Global.RPS), cfg.RateLimits.Global.Burst)
}

func SeckillRateLimit(cfg *config.Config) gin.HandlerFunc {
	return RateLimitMiddleware(rate.Limit(cfg.RateLimits.Seckill.RPS), cfg.RateLimits.Seckill.Burst)
}

func OrderRateLimit(cfg *config.Config) gin.HandlerFunc {
	return RateLimitMiddleware(rate.Limit(cfg.RateLimits.Order.RPS), cfg.RateLimits.Order.Burst)
}
