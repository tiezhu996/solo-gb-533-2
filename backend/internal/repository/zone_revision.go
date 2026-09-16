package repository

import (
	"fmt"

	"gorm.io/gorm"

	"robot-cell-safety-envelope-validator/backend/internal/model"
)

type ZoneRevisionRepository struct{ db *gorm.DB }

func NewZoneRevisionRepository(db *gorm.DB) *ZoneRevisionRepository {
	return &ZoneRevisionRepository{db: db}
}
func (repository *ZoneRevisionRepository) WithDB(db *gorm.DB) *ZoneRevisionRepository {
	return &ZoneRevisionRepository{db: db}
}

func (repository *ZoneRevisionRepository) Create(revision *model.ZoneRevision) error {
	if err := repository.db.Create(revision).Error; err != nil {
		return fmt.Errorf("create zone revision: %w", err)
	}
	return nil
}

func (repository *ZoneRevisionRepository) Update(revision *model.ZoneRevision) error {
	result := repository.db.Model(&model.ZoneRevision{}).
		Where("id = ? AND revision_status = ?", revision.ID, "draft").
		Updates(map[string]any{
			"name": revision.Name, "zone_type": revision.ZoneType, "polygon_geo_json": revision.PolygonGeoJSON,
			"min_height_mm": revision.MinHeightMM, "max_height_mm": revision.MaxHeightMM,
			"speed_limit_mm_s": revision.SpeedLimitMMS, "access_rule": revision.AccessRule,
			"base_version": revision.BaseVersion,
		})
	if result.Error != nil {
		return fmt.Errorf("update zone revision: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrStateConflict
	}
	return nil
}

func (repository *ZoneRevisionRepository) OpenDraft(zoneID uint) (model.ZoneRevision, error) {
	var revision model.ZoneRevision
	if err := repository.db.Preload("SafetyZone").
		Where("safety_zone_id = ? AND revision_status = ?", zoneID, "draft").First(&revision).Error; err != nil {
		return revision, fmt.Errorf("find open zone revision: %w", err)
	}
	return revision, nil
}

// HasOpenDraft reports whether the zone currently holds an unpublished
// revision draft. Such a zone must reject direct updates so the version and
// impact list can only be produced through the publish entry.
func (repository *ZoneRevisionRepository) HasOpenDraft(zoneID uint) (bool, error) {
	var count int64
	if err := repository.db.Model(&model.ZoneRevision{}).
		Where("safety_zone_id = ? AND revision_status = ?", zoneID, "draft").Count(&count).Error; err != nil {
		return false, fmt.Errorf("count open zone revision: %w", err)
	}
	return count > 0, nil
}

func (repository *ZoneRevisionRepository) Get(id uint) (model.ZoneRevision, error) {
	var revision model.ZoneRevision
	if err := repository.db.Preload("SafetyZone").Preload("SafetyZone.RobotCell").First(&revision, id).Error; err != nil {
		return revision, fmt.Errorf("get zone revision: %w", err)
	}
	return revision, nil
}

// GetForZone returns a revision by id, requiring it to belong to the given
// zone, so a revision id from another work-zone path cannot be read here.
func (repository *ZoneRevisionRepository) GetForZone(zoneID, revisionID uint) (model.ZoneRevision, error) {
	var revision model.ZoneRevision
	if err := repository.db.Preload("SafetyZone").Preload("SafetyZone.RobotCell").
		Where("id = ? AND safety_zone_id = ?", revisionID, zoneID).First(&revision).Error; err != nil {
		return revision, fmt.Errorf("get zone revision for zone: %w", err)
	}
	return revision, nil
}

func (repository *ZoneRevisionRepository) ListByZone(zoneID uint) ([]model.ZoneRevision, error) {
	var revisions []model.ZoneRevision
	if err := repository.db.Preload("SafetyZone").Preload("SafetyZone.RobotCell").
		Where("safety_zone_id = ?", zoneID).Order("id DESC").Find(&revisions).Error; err != nil {
		return nil, fmt.Errorf("list zone revisions: %w", err)
	}
	return revisions, nil
}

// Publish advances an open draft to a unique published version using a
// conditional update. The (safety_zone_id, published_version) unique index
// guarantees the version is unique even under concurrent publishes.
func (repository *ZoneRevisionRepository) Publish(id uint, baseVersion, publishedVersion int, publishedAt any) error {
	result := repository.db.Model(&model.ZoneRevision{}).
		Where("id = ? AND revision_status = ? AND base_version = ?", id, "draft", baseVersion).
		Updates(map[string]any{
			"revision_status": "published", "open_draft_slot": nil,
			"published_version": publishedVersion, "published_at": publishedAt,
		})
	if result.Error != nil {
		if IsUniqueViolation(result.Error) {
			return ErrVersionConflict
		}
		return fmt.Errorf("publish zone revision: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrStateConflict
	}
	return nil
}

func (repository *ZoneRevisionRepository) CreateImpact(impact *model.ZoneRevisionImpact) error {
	if err := repository.db.Create(impact).Error; err != nil {
		return fmt.Errorf("create zone revision impact: %w", err)
	}
	return nil
}

func (repository *ZoneRevisionRepository) Impacts(revisionID uint) ([]model.ZoneRevisionImpact, error) {
	var impacts []model.ZoneRevisionImpact
	if err := repository.db.Where("zone_revision_id = ?", revisionID).Order("affected DESC, id ASC").Find(&impacts).Error; err != nil {
		return nil, fmt.Errorf("list zone revision impacts: %w", err)
	}
	return impacts, nil
}

func (repository *ZoneRevisionRepository) CreateReevaluation(flag *model.ValidationReevaluation) error {
	if err := repository.db.Create(flag).Error; err != nil {
		return fmt.Errorf("create validation reevaluation flag: %w", err)
	}
	return nil
}

func (repository *ZoneRevisionRepository) ReevaluationFlags(revisionID uint) ([]model.ValidationReevaluation, error) {
	var flags []model.ValidationReevaluation
	if err := repository.db.Where("zone_revision_id = ?", revisionID).Order("id ASC").Find(&flags).Error; err != nil {
		return nil, fmt.Errorf("list validation reevaluation flags: %w", err)
	}
	return flags, nil
}

// ExistingReevaluationFlag returns the flag already attached to a validation
// run from an earlier published revision.
func (repository *ZoneRevisionRepository) ExistingReevaluationFlag(validationRunID uint) (model.ValidationReevaluation, error) {
	var flag model.ValidationReevaluation
	if err := repository.db.Where("validation_run_id = ?", validationRunID).First(&flag).Error; err != nil {
		return flag, fmt.Errorf("find reevaluation flag: %w", err)
	}
	return flag, nil
}

// ApplyRevisionToZone copies the revised definition onto the live zone row and
// bumps its version conditionally, so the published revision and zone version
// cannot diverge.
func (repository *ZoneRevisionRepository) ApplyRevisionToZone(zoneID uint, expectedVersion, newVersion int, revision model.ZoneRevision) error {
	result := repository.db.Model(&model.SafetyZone{}).
		Where("id = ? AND version = ? AND zone_state <> ?", zoneID, expectedVersion, "inactive").
		Updates(map[string]any{
			"name": revision.Name, "zone_type": revision.ZoneType, "polygon_geo_json": revision.PolygonGeoJSON,
			"min_height_mm": revision.MinHeightMM, "max_height_mm": revision.MaxHeightMM,
			"speed_limit_mm_s": revision.SpeedLimitMMS, "access_rule": revision.AccessRule, "version": newVersion,
		})
	if result.Error != nil {
		return fmt.Errorf("apply revision to safety zone: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrVersionConflict
	}
	return nil
}
