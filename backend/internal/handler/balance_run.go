package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"lng-boiloff-gas-balance/backend/internal/dto"
	"lng-boiloff-gas-balance/backend/internal/repository"
	"lng-boiloff-gas-balance/backend/internal/service"
	"lng-boiloff-gas-balance/backend/pkg/api"
)

type BalanceHandler struct {
	service *service.BalanceService
}

func NewBalanceHandler(balanceService *service.BalanceService) *BalanceHandler {
	return &BalanceHandler{service: balanceService}
}

func (h *BalanceHandler) List(c *gin.Context) {
	page, pageSize := pagination(c)
	tankID, _ := strconv.ParseUint(c.Query("tank_id"), 10, 32)
	filter := repository.BalanceFilter{TankID: uint(tankID), Status: c.Query("status"), Page: page, PageSize: pageSize}
	items, total, err := h.service.List(c.Request.Context(), filter)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.Page(c, items, page, pageSize, total)
}

func (h *BalanceHandler) Get(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	item, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.Success(c, http.StatusOK, item)
}

func (h *BalanceHandler) Run(c *gin.Context) {
	actor, ok := actorFromContext(c)
	if !ok {
		return
	}
	var request dto.RunBalanceRequest
	if !bindJSON(c, &request) {
		return
	}
	item, err := h.service.Run(c.Request.Context(), request, actor)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.Success(c, http.StatusCreated, item)
}

func (h *BalanceHandler) BoundaryPreview(c *gin.Context) {
	tankID, err := strconv.ParseUint(c.Query("tank_id"), 10, 32)
	if err != nil || tankID == 0 {
		api.Fail(c, api.NewError(400, "INVALID_TANK_ID", "必须提供有效的储罐 ID"))
		return
	}
	start, ok := requiredTimeQuery(c, "period_start")
	if !ok {
		return
	}
	end, ok := requiredTimeQuery(c, "period_end")
	if !ok {
		return
	}
	preview, err := h.service.BoundaryPreview(c.Request.Context(), uint(tankID), start, end)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.Success(c, http.StatusOK, preview)
}

func requiredTimeQuery(c *gin.Context, name string) (time.Time, bool) {
	value := strings.TrimSpace(c.Query(name))
	if value == "" {
		api.Fail(c, api.WithDetails(api.NewError(400, "BALANCE_PERIOD_REQUIRED", "必须提供平衡期间起止时间"), map[string]any{"field": name}))
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		api.Fail(c, api.WithDetails(api.NewError(400, "INVALID_TIME_FILTER", "期间时间必须使用 RFC3339 格式"), map[string]any{"field": name}))
		return time.Time{}, false
	}
	return parsed.UTC(), true
}

func (h *BalanceHandler) Submit(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	actor, ok := actorFromContext(c)
	if !ok {
		return
	}
	var request dto.SubmitBalanceRequest
	if !bindJSON(c, &request) {
		return
	}
	item, err := h.service.Submit(c.Request.Context(), id, request, actor)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.Success(c, http.StatusOK, item)
}

func (h *BalanceHandler) Review(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	actor, ok := actorFromContext(c)
	if !ok {
		return
	}
	var request dto.ReviewBalanceRequest
	if !bindJSON(c, &request) {
		return
	}
	item, err := h.service.Review(c.Request.Context(), id, request, actor)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.Success(c, http.StatusOK, item)
}

func (h *BalanceHandler) Invalidate(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	actor, ok := actorFromContext(c)
	if !ok {
		return
	}
	var request dto.InvalidateBalanceRequest
	if !bindJSON(c, &request) {
		return
	}
	item, err := h.service.Invalidate(c.Request.Context(), id, request, actor)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.Success(c, http.StatusOK, item)
}

func (h *BalanceHandler) Uncertainty(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	result, err := h.service.Uncertainty(c.Request.Context(), id)
	if err != nil {
		api.Fail(c, err)
		return
	}
	api.Success(c, http.StatusOK, result)
}
