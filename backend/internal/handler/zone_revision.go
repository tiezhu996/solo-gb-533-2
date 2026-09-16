package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"robot-cell-safety-envelope-validator/backend/internal/dto"
	"robot-cell-safety-envelope-validator/backend/internal/service"
)

type ZoneRevisionHandler struct{ service *service.ZoneRevisionService }

func NewZoneRevisionHandler(service *service.ZoneRevisionService) *ZoneRevisionHandler {
	return &ZoneRevisionHandler{service: service}
}

func (handler *ZoneRevisionHandler) List(context *gin.Context) {
	id, err := PathID(context)
	if err != nil {
		WriteError(context, err)
		return
	}
	items, err := handler.service.List(id)
	if err != nil {
		WriteError(context, err)
		return
	}
	WriteData(context, http.StatusOK, items)
}

func (handler *ZoneRevisionHandler) OpenDraft(context *gin.Context) {
	id, err := PathID(context)
	if err != nil {
		WriteError(context, err)
		return
	}
	item, err := handler.service.OpenDraft(id)
	if err != nil {
		WriteError(context, err)
		return
	}
	WriteData(context, http.StatusOK, item)
}

func (handler *ZoneRevisionHandler) SaveDraft(context *gin.Context) {
	id, err := PathID(context)
	if err != nil {
		WriteError(context, err)
		return
	}
	var request dto.SaveZoneRevisionDraftRequest
	if err := BindAndValidate(context, &request); err != nil {
		WriteError(context, err)
		return
	}
	item, err := handler.service.SaveDraft(id, request, Actor(context), RequestID(context))
	if err != nil {
		WriteError(context, err)
		return
	}
	WriteData(context, http.StatusOK, item)
}

func (handler *ZoneRevisionHandler) Publish(context *gin.Context) {
	id, err := PathID(context)
	if err != nil {
		WriteError(context, err)
		return
	}
	var request dto.PublishZoneRevisionRequest
	if err := BindAndValidate(context, &request); err != nil {
		WriteError(context, err)
		return
	}
	item, err := handler.service.Publish(id, request, Actor(context), RequestID(context))
	if err != nil {
		WriteError(context, err)
		return
	}
	WriteData(context, http.StatusOK, item)
}

func (handler *ZoneRevisionHandler) Get(context *gin.Context) {
	zoneID, err := PathID(context)
	if err != nil {
		WriteError(context, err)
		return
	}
	revisionID, err := PathParamID(context, "revisionId")
	if err != nil {
		WriteError(context, err)
		return
	}
	item, err := handler.service.GetForZone(zoneID, revisionID)
	if err != nil {
		WriteError(context, err)
		return
	}
	WriteData(context, http.StatusOK, item)
}
