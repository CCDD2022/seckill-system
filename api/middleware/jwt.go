package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/CCDD2022/seckill-system/pkg/e"
	"github.com/CCDD2022/seckill-system/pkg/utils"
	"github.com/gin-gonic/gin"
)

// JWTAuthMiddleware JWT认证中间件
func JWTAuthMiddleware(jwtUtil *utils.JWTUtil) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    e.ERROR_AUTH,
				"message": e.GetMsg(e.ERROR_AUTH),
			})
			c.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    e.ERROR_AUTH,
				"message": "Invalid Authorization format",
			})
			c.Abort()
			return
		}

		claims, err := jwtUtil.ParseToken(parts[1])
		if err != nil {
			if errors.Is(err, utils.ErrTokenExpired) {
				c.JSON(http.StatusUnauthorized, gin.H{
					"code":    e.ERROR_AUTH_CHECK_TOKEN_TIMEOUT,
					"message": e.GetMsg(e.ERROR_AUTH_CHECK_TOKEN_TIMEOUT),
				})
			} else {
				c.JSON(http.StatusUnauthorized, gin.H{
					"code":    e.ERROR_AUTH_CHECK_TOKEN_FAIL,
					"message": e.GetMsg(e.ERROR_AUTH_CHECK_TOKEN_FAIL),
				})
			}
			c.Abort()
			return
		}

		// 注入用户信息
		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)

		c.Next()
	}
}
