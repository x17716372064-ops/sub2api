package admin

import (
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// GetUpstreamAccountBalance returns the write-only configuration metadata and
// latest balance snapshot for an account. The password is represented only by
// password_configured and is never returned.
func (h *AccountHandler) GetUpstreamAccountBalance(c *gin.Context) {
	if h == nil || h.upstreamAccountBalance == nil {
		response.ErrorFrom(c, service.ErrUpstreamAccountBalanceUnavailable)
		return
	}
	accountID, ok := parseUpstreamBalanceAccountID(c)
	if !ok {
		return
	}
	state, err := h.upstreamAccountBalance.GetState(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, state)
}

// SaveUpstreamAccountBalance saves website/email/password and immediately
// performs one balance refresh. An empty website clears the configuration.
func (h *AccountHandler) SaveUpstreamAccountBalance(c *gin.Context) {
	if h == nil || h.upstreamAccountBalance == nil {
		response.ErrorFrom(c, service.ErrUpstreamAccountBalanceUnavailable)
		return
	}
	accountID, ok := parseUpstreamBalanceAccountID(c)
	if !ok {
		return
	}
	var req service.UpstreamAccountBalanceSaveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	state, err := h.upstreamAccountBalance.SaveConfig(c.Request.Context(), accountID, req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if !state.Configured {
		response.Success(c, state)
		return
	}
	refreshed, err := h.upstreamAccountBalance.Refresh(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, refreshed)
}

// RefreshUpstreamAccountBalance performs a manual query using the encrypted
// credentials already stored for the account.
func (h *AccountHandler) RefreshUpstreamAccountBalance(c *gin.Context) {
	if h == nil || h.upstreamAccountBalance == nil {
		response.ErrorFrom(c, service.ErrUpstreamAccountBalanceUnavailable)
		return
	}
	accountID, ok := parseUpstreamBalanceAccountID(c)
	if !ok {
		return
	}
	state, err := h.upstreamAccountBalance.Refresh(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, state)
}

func parseUpstreamBalanceAccountID(c *gin.Context) (int64, bool) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.Error(c, http.StatusBadRequest, "Invalid account ID")
		return 0, false
	}
	return accountID, true
}
