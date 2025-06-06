package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// JWTClaims JWT声明
type JWTClaims struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// Login 用户登录
func Login(ctx *Context) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req LoginRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, Response{
				Code:    400,
				Message: "Invalid request format",
			})
			return
		}

		// 验证用户名和密码
		if !validateCredentials(ctx, req.Username, req.Password) {
			c.JSON(http.StatusUnauthorized, Response{
				Code:    401,
				Message: "Invalid username or password",
			})
			return
		}

		// 生成JWT令牌
		token, expiresAt, err := generateJWT(ctx, req.Username)
		if err != nil {
			ctx.Logger.Errorf("Failed to generate JWT: %v", err)
			c.JSON(http.StatusInternalServerError, Response{
				Code:    500,
				Message: "Failed to generate token",
			})
			return
		}

		response := LoginResponse{
			Token:     token,
			ExpiresAt: expiresAt,
			User: User{
				Username: req.Username,
				Role:     "admin",
			},
		}

		ctx.Logger.Infof("User %s logged in successfully", req.Username)
		c.JSON(http.StatusOK, Response{
			Code:    200,
			Message: "Login successful",
			Data:    response,
		})
	}
}

// Logout 用户登出
func Logout(ctx *Context) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 在实际应用中，这里可以将token加入黑名单
		c.JSON(http.StatusOK, Response{
			Code:    200,
			Message: "Logout successful",
		})
	}
}

// GetCurrentUser 获取当前用户信息
func GetCurrentUser(ctx *Context) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从上下文中获取用户信息
		username, exists := c.Get("username")
		if !exists {
			c.JSON(http.StatusUnauthorized, Response{
				Code:    401,
				Message: "User not authenticated",
			})
			return
		}

		user := User{
			Username: username.(string),
			Role:     "admin",
		}

		c.JSON(http.StatusOK, Response{
			Code:    200,
			Message: "Success",
			Data:    user,
		})
	}
}

// AuthMiddleware 认证中间件
func AuthMiddleware(ctx *Context) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取Authorization头
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, Response{
				Code:    401,
				Message: "Authorization header required",
			})
			c.Abort()
			return
		}

		// 检查Bearer前缀
		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString == authHeader {
			c.JSON(http.StatusUnauthorized, Response{
				Code:    401,
				Message: "Invalid authorization format",
			})
			c.Abort()
			return
		}

		// 验证JWT令牌
		claims, err := validateJWT(ctx, tokenString)
		if err != nil {
			c.JSON(http.StatusUnauthorized, Response{
				Code:    401,
				Message: "Invalid or expired token",
			})
			c.Abort()
			return
		}

		// 将用户信息存储到上下文中
		c.Set("username", claims.Username)
		c.Set("role", claims.Role)

		c.Next()
	}
}

// validateCredentials 验证用户凭据
func validateCredentials(ctx *Context, username, password string) bool {
	// 简单的硬编码验证，实际应用中应该从数据库验证
	if username != ctx.Config.Admin.Username {
		return false
	}

	// 如果配置中的密码是明文，直接比较
	// 在生产环境中，应该使用哈希密码
	if password == ctx.Config.Admin.Password {
		return true
	}

	// 尝试bcrypt验证（如果配置中存储的是哈希密码）
	err := bcrypt.CompareHashAndPassword([]byte(ctx.Config.Admin.Password), []byte(password))
	return err == nil
}

// generateJWT 生成JWT令牌
func generateJWT(ctx *Context, username string) (string, int64, error) {
	expiresAt := time.Now().Add(24 * time.Hour)

	claims := JWTClaims{
		Username: username,
		Role:     "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "nsa-service",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(ctx.Config.Admin.JWTSecret))
	if err != nil {
		return "", 0, err
	}

	return tokenString, expiresAt.Unix(), nil
}

// validateJWT 验证JWT令牌
func validateJWT(ctx *Context, tokenString string) (*JWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(ctx.Config.Admin.JWTSecret), nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*JWTClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, jwt.ErrTokenInvalidClaims
}
