package router

import (
	"github.com/gin-gonic/gin"

	"robot-cell-safety-envelope-validator/backend/internal/constants"
	"robot-cell-safety-envelope-validator/backend/internal/handler"
	"robot-cell-safety-envelope-validator/backend/internal/middleware"
)

func registerZoneRevisionRoutes(group *gin.RouterGroup, target *handler.ZoneRevisionHandler) {
	routes := group.Group("/zones")
	routes.GET("/:id/revisions", target.List)
	routes.GET("/:id/revisions/draft", target.OpenDraft)
	routes.GET("/:id/revisions/:revisionId", target.Get)
	// Draft editing and the publish action are the only mutation paths. Publish
	// is the single execution entry that finalizes a version and its impact list.
	write := routes.Group("")
	write.Use(middleware.RBAC(constants.RoleSafetyEngineer, constants.RoleAdmin))
	write.PUT("/:id/revisions/draft", middleware.RateLimit(30, "revision"), target.SaveDraft)
	write.POST("/:id/revisions/publish", middleware.RateLimit(30, "revision"), target.Publish)
}
