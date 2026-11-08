// Package web 是数据库操作服务的 HTTP 入口：迁移、台账、回滚、健康检查。
package web

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/lifeipkg/rcp-db-ops/internal/migration"
)

// Pinger 只用于健康检查：探活要反映数据库是否可达。
type Pinger interface {
	Ping(ctx context.Context) error
}

type Handler struct {
	service *migration.Service
	pinger  Pinger
}

func NewHandler(service *migration.Service, pinger Pinger) *Handler {
	return &Handler{service: service, pinger: pinger}
}

// NewRouter 注册路由。
//
// 路径写成完整字面量：rcp-contracts/tools/contract_check.py 用正则抽 r.GET/r.POST
// 与 OpenAPI 做两向差集。探活用 r.Handle 注册，不计入契约路由（不在公开契约内）。
func NewRouter(h *Handler) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	r.POST("/dbops/migrate", h.migrate)
	r.GET("/dbops/versions", h.versions)
	r.POST("/dbops/rollback", h.rollback)
	r.Handle(http.MethodGet, "/actuator/health", h.health)
	return r
}

// migrateRequest 的 targetVersion 缺省表示迁移到最新。
type migrateRequest struct {
	TargetVersion string `json:"targetVersion"`
}

// rollbackRequest 的 targetVersion 空串表示全部回滚。
type rollbackRequest struct {
	TargetVersion string `json:"targetVersion"`
}

type errorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func (h *Handler) migrate(c *gin.Context) {
	var req migrateRequest
	// 契约允许不带请求体；空 body 不是错误，其余解析失败才是
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "BAD_REQUEST", Message: "请求体不是合法 JSON"})
		return
	}
	result, err := h.service.Migrate(c.Request.Context(), req.TargetVersion)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) versions(c *gin.Context) {
	result, err := h.service.Status(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) rollback(c *gin.Context) {
	var req rollbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "BAD_REQUEST", Message: "请求体不是合法 JSON"})
		return
	}
	result, err := h.service.RollbackTo(c.Request.Context(), req.TargetVersion)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) health(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if err := h.pinger.Ping(ctx); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "DOWN"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "UP"})
}

// writeError 把迁移领域错误翻译成 400 + 编码化原因；其他错误一律 500。
func writeError(c *gin.Context, err error) {
	var domain *migration.Error
	if errors.As(err, &domain) {
		c.JSON(http.StatusBadRequest, errorResponse{Error: domain.Code, Message: domain.Message})
		return
	}
	// 数据库错误信息可能含表名与 SQL 片段，只进服务端日志
	log.Printf("[db-ops] %v", err)
	c.JSON(http.StatusInternalServerError, errorResponse{
		Error:   "INTERNAL_ERROR",
		Message: "迁移执行失败",
	})
}
