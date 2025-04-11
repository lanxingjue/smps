// api/middleware/auth.go
package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/gin-gonic/gin"
)

// JWTConfig JWT配置
type JWTConfig struct {
	SecretKey     string
	TokenExpiry   time.Duration
	TokenLookup   string
	TokenHeadName string
}

// DefaultJWTConfig 默认JWT配置
func DefaultJWTConfig() JWTConfig {
	return JWTConfig{
		SecretKey:     "smps-jwt-secret-key",
		TokenExpiry:   24 * time.Hour,
		TokenLookup:   "header: Authorization, query: token, cookie: jwt",
		TokenHeadName: "Bearer",
	}
}

// JWTAuth JWT认证中间件
func JWTAuth(config ...JWTConfig) gin.HandlerFunc {
	var cfg JWTConfig
	if len(config) > 0 {
		cfg = config[0]
	} else {
		cfg = DefaultJWTConfig()
	}

	return func(c *gin.Context) {
		// 对登录和静态资源接口放行
		if c.FullPath() == "/api/auth/login" ||
			c.FullPath() == "/api/auth/logout" ||
			strings.HasPrefix(c.FullPath(), "/static") {
			c.Next()
			return
		}

		// 获取token
		token := extractToken(c, cfg.TokenLookup, cfg.TokenHeadName)
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "未授权，需要登录",
			})
			c.Abort()
			return
		}

		// 验证token
		claims, err := validateToken(token, cfg.SecretKey)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "无效的令牌或令牌已过期",
			})
			c.Abort()
			return
		}

		// 设置用户信息到上下文
		c.Set("username", claims.Username)
		c.Set("role", claims.Role)

		c.Next()
	}
}

// JWT声明
type JWTClaims struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.StandardClaims
}

// 提取token
func extractToken(c *gin.Context, lookup, headName string) string {
	methods := strings.Split(lookup, ",")

	for _, method := range methods {
		parts := strings.Split(strings.TrimSpace(method), ":")
		if len(parts) != 2 {
			continue
		}

		source := strings.TrimSpace(parts[0])
		key := strings.TrimSpace(parts[1])

		switch source {
		case "header":
			token := c.GetHeader(key)
			if len(token) > len(headName) && strings.ToLower(token[0:len(headName)]) == strings.ToLower(headName) {
				return token[len(headName)+1:]
			}
		case "query":
			token := c.Query(key)
			if token != "" {
				return token
			}
		case "cookie":
			token, _ := c.Cookie(key)
			if token != "" {
				return token
			}
		}
	}

	return ""
}

// 验证token
func validateToken(tokenString, secret string) (*JWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*JWTClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, jwt.ErrSignatureInvalid
}

// GenerateToken 生成JWT令牌
func GenerateToken(username, role, secret string, expiry time.Duration) (string, error) {
	claims := JWTClaims{
		Username: username,
		Role:     role,
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: time.Now().Add(expiry).Unix(),
			IssuedAt:  time.Now().Unix(),
			Subject:   username,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}
