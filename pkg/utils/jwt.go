package utils

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

// 可复用的错误定义
var (
	ErrTokenExpired = errors.New("token expired")
	ErrTokenInvalid = errors.New("token invalid")
)

// JWTUtil JWT配置结构体
type JWTUtil struct {
	secret     string
	expireTime time.Duration
}

func NewJWTUtil(secret string, expireHours int) *JWTUtil {
	return &JWTUtil{
		secret:     secret,
		expireTime: time.Duration(expireHours) * time.Hour,
	}
}

type Claims struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// GenerateToken 生成 JWT token，返回token和过期时间
func (j *JWTUtil) GenerateToken(userID int64, username string) (string, error) {
	expiresAt := time.Now().Add(j.expireTime)
	claims := Claims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(j.secret))
	if err != nil {
		return "", err
	}
	return signed, nil
}

// ParseToken 解析 JWT token
func (j *JWTUtil) ParseToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(j.secret), nil
	})

	if err != nil {
		// 过期错误识别
		if errors.Is(err, jwt.ErrTokenExpired) || errors.Is(err, jwt.ErrTokenInvalidClaims) {
			return nil, ErrTokenExpired
		}
		return nil, ErrTokenInvalid
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}
	return nil, ErrTokenInvalid
}
