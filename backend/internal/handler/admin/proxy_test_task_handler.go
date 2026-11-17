package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type ProxyTestTaskHandler struct{ service *service.ProxyTestTaskService }

func NewProxyTestTaskHandler(service *service.ProxyTestTaskService) *ProxyTestTaskHandler {
	return &ProxyTestTaskHandler{service: service}
}

func (h *ProxyTestTaskHandler) List(c *gin.Context) {
	tasks, err := h.service.List(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, tasks)
}

func (h *ProxyTestTaskHandler) Create(c *gin.Context) {
	var input service.ProxyTestTaskInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	task, err := h.service.Create(c.Request.Context(), &input)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, task)
}

func proxyTaskID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid proxy test task ID")
		return 0, false
	}
	return id, true
}

func (h *ProxyTestTaskHandler) Update(c *gin.Context) {
	id, ok := proxyTaskID(c)
	if !ok {
		return
	}
	var input service.ProxyTestTaskInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	task, err := h.service.Update(c.Request.Context(), id, &input)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, task)
}

func (h *ProxyTestTaskHandler) Delete(c *gin.Context) {
	id, ok := proxyTaskID(c)
	if !ok {
		return
	}
	if err := h.service.Delete(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "Proxy test task deleted"})
}

func (h *ProxyTestTaskHandler) Run(c *gin.Context) {
	id, ok := proxyTaskID(c)
	if !ok {
		return
	}
	task, err := h.service.Run(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, task)
}

func (h *ProxyTestTaskHandler) SendTestEmail(c *gin.Context) {
	id, ok := proxyTaskID(c)
	if !ok {
		return
	}
	if err := h.service.SendTestEmail(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "Test email sent"})
}
