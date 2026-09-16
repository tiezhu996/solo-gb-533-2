package model

import "time"

// ZoneRevision is the single unpublished revision draft (or a published
// revision) of one safety zone. At most one open draft may exist per zone,
// enforced by a partial unique index on (safety_zone_id, open_draft_slot).
type ZoneRevision struct {
	ID               uint      `gorm:"primaryKey"`
	SafetyZoneID     uint      `gorm:"index:idx_revision_zone;uniqueIndex:idx_revision_open_draft,priority:1;uniqueIndex:idx_revision_published_version,priority:1;not null"`
	RobotCellID      uint      `gorm:"index;not null"`
	RevisionStatus   string    `gorm:"size:24;index;not null"`
	OpenDraftSlot    *int      `gorm:"uniqueIndex:idx_revision_open_draft,priority:2"`
	BaseVersion      int       `gorm:"not null"`
	PublishedVersion *int      `gorm:"uniqueIndex:idx_revision_published_version,priority:2"`
	Name             string    `gorm:"size:120;not null"`
	ZoneType         string    `gorm:"size:24;not null"`
	PolygonGeoJSON   string    `gorm:"type:text;not null"`
	MinHeightMM      float64   `gorm:"not null"`
	MaxHeightMM      float64   `gorm:"not null"`
	SpeedLimitMMS    float64   `gorm:"column:speed_limit_mm_s;not null"`
	AccessRule       string    `gorm:"size:300;not null"`
	CreatedBy        uint      `gorm:"index;not null"`
	CreatedAt        time.Time `gorm:"not null"`
	UpdatedAt        time.Time `gorm:"not null"`
	PublishedAt      *time.Time
	SafetyZone       SafetyZone `gorm:"foreignKey:SafetyZoneID"`
}

// ZoneRevisionImpact lists every active motion program in the work cell that
// the revision touches, computed at publish time and frozen with the revision.
type ZoneRevisionImpact struct {
	ID              uint          `gorm:"primaryKey"`
	ZoneRevisionID  uint          `gorm:"uniqueIndex:idx_revision_impact_program,priority:1;index;not null"`
	MotionProgramID uint          `gorm:"uniqueIndex:idx_revision_impact_program,priority:2;index;not null"`
	ProgramCode     string        `gorm:"size:80;not null"`
	ProgramVersion  int           `gorm:"not null"`
	ProgramState    string        `gorm:"size:24;not null"`
	Affected        bool          `gorm:"not null"`
	CollisionCount  int           `gorm:"not null"`
	FirstSegment    int           `gorm:"not null;default:-1"`
	FirstTimeMS     float64       `gorm:"not null;default:-1"`
	MinClearanceMM  float64       `gorm:"not null"`
	ActualSpeedMMS  float64       `gorm:"not null"`
	AllowedSpeedMMS float64       `gorm:"not null"`
	Evidence        string        `gorm:"size:500;not null"`
	CreatedAt       time.Time     `gorm:"not null"`
	ZoneRevision    ZoneRevision  `gorm:"foreignKey:ZoneRevisionID"`
	MotionProgram   MotionProgram `gorm:"foreignKey:MotionProgramID"`
}

// ValidationReevaluation flags a previously accepted validation that must be
// re-evaluated after a published revision. The validation record itself and its
// historical evidence are never modified; this marker is independently readable.
type ValidationReevaluation struct {
	ID              uint          `gorm:"primaryKey"`
	ZoneRevisionID  uint          `gorm:"index;not null"`
	ValidationRunID uint          `gorm:"uniqueIndex:idx_reevaluation_run;index;not null"`
	MotionProgramID uint          `gorm:"index;not null"`
	PriorStatus     string        `gorm:"size:24;not null"`
	FlagReason      string        `gorm:"size:300;not null"`
	CreatedAt       time.Time     `gorm:"not null"`
	ZoneRevision    ZoneRevision  `gorm:"foreignKey:ZoneRevisionID"`
	ValidationRun   ValidationRun `gorm:"foreignKey:ValidationRunID"`
}
