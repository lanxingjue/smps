// api/handlers/auth.go
package handlers

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// AuthHandler 认证处理器
type AuthHandler struct {
	db *sql.DB
}

// NewAuthHandler 创建认证处理器
func NewAuthHandler(db *sql.DB) *AuthHandler {
	return &AuthHandler{
		db: db,
	}
}

// LoginRequest 登录请求
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// // Login 登录处理
// func (h *AuthHandler) Login(c *gin.Context) {
// 	var req LoginRequest
// 	if err := c.ShouldBindJSON(&req); err != nil {
// 		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
// 		return
// 	}

// 	// 简单认证，实际项目中应该查询数据库
// 	if req.Username == "admin" && req.Password == "admin123" {
// 		// 生成JWT令牌
// 		token, err := middleware.GenerateToken(req.Username, "admin", "smps-jwt-secret-key", 24*time.Hour)
// 		if err != nil {
// 			c.JSON(http.StatusInternalServerError, gin.H{"error": "生成令牌失败"})
// 			return
// 		}

// 		c.JSON(http.StatusOK, gin.H{
// 			"token":  token,
// 			"expire": time.Now().Add(24 * time.Hour).Unix(),
// 			"user": gin.H{
// 				"username": req.Username,
// 				"role":     "admin",
// 			},
// 		})
// 	} else {
// 		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
// 	}
// }

// // Logout 登出处理
// func (h *AuthHandler) Logout(c *gin.Context) {
// 	c.JSON(http.StatusOK, gin.H{"message": "登出成功"})
// }

// Login 简化的登录处理
func (h *AuthHandler) Login(c *gin.Context) {
	// 直接返回成功响应
	c.JSON(http.StatusOK, gin.H{
		"token":  "no-auth-required-token",
		"expire": time.Now().Add(365 * 24 * time.Hour).Unix(), // 设置一个很长的过期时间
		"user": gin.H{
			"username": "admin",
			"role":     "admin",
		},
	})
}

// Logout 简化的登出处理
func (h *AuthHandler) Logout(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "登出成功"})
}
