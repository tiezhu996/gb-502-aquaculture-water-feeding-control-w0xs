package handler

import (
	"aquaculture-water-feeding-control/backend/internal/dto"
	"aquaculture-water-feeding-control/backend/internal/service"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type RestrictionHandler struct {
	service *service.RestrictionService
}

func NewRestrictionHandler(restrictions *service.RestrictionService) *RestrictionHandler {
	return &RestrictionHandler{service: restrictions}
}

func (h *RestrictionHandler) List(c *gin.Context) {
	query, ok := bindPageQuery(c)
	if !ok {
		return
	}
	pondID, _ := strconv.ParseUint(c.Query("pondId"), 10, 64)
	result, err := h.service.List(query, uint(pondID), c.Query("status"))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *RestrictionHandler) ActiveByPond(c *gin.Context) {
	pondID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || pondID == 0 {
		respondError(c, service.NewError(service.CodeValidation, "养殖池 ID 无效"))
		return
	}
	result, active, err := h.service.ActiveForPond(uint(pondID))
	if err != nil {
		respondError(c, err)
		return
	}
	if !active {
		c.JSON(http.StatusOK, gin.H{"data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *RestrictionHandler) Get(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	result, err := h.service.Get(id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *RestrictionHandler) Dispose(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var input dto.DisposeRestrictionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, service.NewError(service.CodeValidation, "请填写至少 5 个字的处置说明"))
		return
	}
	result, err := h.service.Dispose(id, input.DispositionNote, actorFromContext(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *RestrictionHandler) Release(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var input dto.ReleaseRestrictionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, service.NewError(service.CodeValidation, "请填写至少 5 个字的复核依据"))
		return
	}
	result, err := h.service.Release(id, input.ReleaseBasis, actorFromContext(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}
