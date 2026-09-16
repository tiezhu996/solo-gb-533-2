package dto

import (
	"encoding/json"
	"time"
)

type SaveZoneRevisionDraftRequest struct {
	Name            string          `json:"name" validate:"required,min=2,max=120"`
	ZoneType        string          `json:"zone_type" validate:"required"`
	PolygonGeoJSON  json.RawMessage `json:"polygon_geojson" validate:"required"`
	MinHeightMM     float64         `json:"min_height_mm" validate:"gte=0,lte=30000"`
	MaxHeightMM     float64         `json:"max_height_mm" validate:"required,gt=0,lte=30000"`
	SpeedLimitMMS   float64         `json:"speed_limit_mm_s" validate:"gte=0,lte=10000"`
	AccessRule      string          `json:"access_rule" validate:"required,max=300"`
	ExpectedVersion int             `json:"expected_version" validate:"gte=0"`
}

type PublishZoneRevisionRequest struct {
	ExpectedVersion int `json:"expected_version" validate:"gte=0"`
}

type ZoneRevisionImpactItem struct {
	MotionProgramID uint    `json:"motion_program_id"`
	ProgramCode     string  `json:"program_code"`
	ProgramVersion  int     `json:"program_version"`
	ProgramState    string  `json:"program_state"`
	Affected        bool    `json:"affected"`
	CollisionCount  int     `json:"collision_count"`
	FirstSegment    int     `json:"first_segment"`
	FirstTimeMS     float64 `json:"first_time_ms"`
	MinClearanceMM  float64 `json:"min_clearance_mm"`
	ActualSpeedMMS  float64 `json:"actual_speed_mm_s"`
	AllowedSpeedMMS float64 `json:"allowed_speed_mm_s"`
	Evidence        string  `json:"evidence"`
}

type ReevaluationFlagItem struct {
	ValidationRunID uint      `json:"validation_run_id"`
	MotionProgramID uint      `json:"motion_program_id"`
	PriorStatus     string    `json:"prior_status"`
	FlagReason      string    `json:"flag_reason"`
	CreatedAt       time.Time `json:"created_at"`
}

type ZoneRevisionResponse struct {
	ID                uint                     `json:"id"`
	SafetyZoneID      uint                     `json:"safety_zone_id"`
	RobotCellID       uint                     `json:"robot_cell_id"`
	RobotCellCode     string                   `json:"robot_cell_code"`
	ZoneName          string                   `json:"zone_name"`
	RevisionStatus    string                   `json:"revision_status"`
	BaseVersion       int                      `json:"base_version"`
	PublishedVersion  *int                     `json:"published_version"`
	Name              string                   `json:"name"`
	ZoneType          string                   `json:"zone_type"`
	PolygonGeoJSON    json.RawMessage          `json:"polygon_geojson"`
	MinHeightMM       float64                  `json:"min_height_mm"`
	MaxHeightMM       float64                  `json:"max_height_mm"`
	SpeedLimitMMS     float64                  `json:"speed_limit_mm_s"`
	AccessRule        string                   `json:"access_rule"`
	CreatedAt         time.Time                `json:"created_at"`
	UpdatedAt         time.Time                `json:"updated_at"`
	PublishedAt       *time.Time               `json:"published_at"`
	Impacts           []ZoneRevisionImpactItem `json:"impacts"`
	ReevaluationFlags []ReevaluationFlagItem   `json:"reevaluation_flags"`
}
