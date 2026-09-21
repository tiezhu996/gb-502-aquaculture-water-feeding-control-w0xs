package handler

import (
	"aquaculture-water-feeding-control/backend/internal/constants"
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
	status := constants.RestrictionStatus(c.Query("status"))
	if status != "" && !status.Valid() {
		respondError(c, service.NewError(service.CodeValidation, "停喂限制状态无效"))
		return
	}
	result, err := h.service.List(query, uint(pondID), status)
	if err != nil {
		respondError(c, err)
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

func (h *RestrictionHandler) Handle(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var input dto.HandleRestrictionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, service.NewError(service.CodeValidation, "请填写至少 2 个字的处置说明"))
		return
	}
	result, err := h.service.Handle(id, input.Note, actorFromContext(c))
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
		respondError(c, service.NewError(service.CodeValidation, "请填写至少 2 个字的复核依据"))
		return
	}
	result, err := h.service.Release(id, input.ReviewNote, actorFromContext(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}
