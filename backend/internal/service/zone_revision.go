package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"gorm.io/gorm"

	"robot-cell-safety-envelope-validator/backend/internal/constants"
	"robot-cell-safety-envelope-validator/backend/internal/dto"
	"robot-cell-safety-envelope-validator/backend/internal/geometry"
	"robot-cell-safety-envelope-validator/backend/internal/model"
	"robot-cell-safety-envelope-validator/backend/internal/repository"
)

// ZoneRevisionService is the single execution entry for safety-zone revisions.
// A draft is the only mutable artifact (one per zone); publish is one atomic
// transaction that fixes the zone version and the impact list together and only
// flags previously accepted validations for re-evaluation.
type ZoneRevisionService struct {
	db          *gorm.DB
	revisions   *repository.ZoneRevisionRepository
	zones       *repository.SafetyZoneRepository
	programs    *repository.MotionProgramRepository
	validations *repository.ValidationRunRepository
	system      *SystemService
	// faultAtStage is zero outside tests. When set to a publish stage, Publish
	// aborts that stage inside the transaction so the failure (and its stage)
	// can be exercised deterministically.
	faultAtStage string
}

// Publish stages, named so an injected or real failure can be attributed.
const (
	stageLoadActivePrograms = "load_active_programs"
	stageApplyZoneVersion   = "apply_zone_version"
	stagePublishDraft       = "publish_draft"
	stagePersistImpacts     = "persist_impacts"
	stagePersistReevalFlags = "persist_reevaluation_flags"
	stageRecordPublishAudit = "record_publish_audit"
)

// revisionPublishError identifies the publish stage at which a failure was
// injected. It always occurs inside the transaction and therefore forces a
// full rollback.
type revisionPublishError struct{ stage string }

func (err *revisionPublishError) Error() string {
	return "injected failure at publish stage: " + err.stage
}
func (err *revisionPublishError) Stage() string { return err.stage }

func NewZoneRevisionService(db *gorm.DB, revisions *repository.ZoneRevisionRepository, zones *repository.SafetyZoneRepository, programs *repository.MotionProgramRepository, validations *repository.ValidationRunRepository, system *SystemService) *ZoneRevisionService {
	return &ZoneRevisionService{db: db, revisions: revisions, zones: zones, programs: programs, validations: validations, system: system}
}

// setFaultAtStage injects a failure at one named publish stage. Test-only: the
// production constructor leaves it empty and the hook is a no-op.
func (service *ZoneRevisionService) setFaultAtStage(stage string) { service.faultAtStage = stage }

func (service *ZoneRevisionService) faultHook(stage string) error {
	if service.faultAtStage == stage {
		return &revisionPublishError{stage: stage}
	}
	return nil
}

func (service *ZoneRevisionService) SaveDraft(zoneID uint, request dto.SaveZoneRevisionDraftRequest, actor dto.Actor, requestID string) (dto.ZoneRevisionResponse, error) {
	if err := validateZone(request.ZoneType, request.PolygonGeoJSON, request.MinHeightMM, request.MaxHeightMM, request.SpeedLimitMMS); err != nil {
		return dto.ZoneRevisionResponse{}, err
	}
	zone, err := service.zones.Get(zoneID)
	if err != nil {
		return dto.ZoneRevisionResponse{}, MapRepositoryError("safety zone", err)
	}
	if zone.ZoneState == constants.ZoneStateInactive {
		return dto.ZoneRevisionResponse{}, Conflict("zone_inactive", "an inactive zone cannot hold a revision draft", repository.ErrStateConflict)
	}
	if request.ExpectedVersion > 0 && request.ExpectedVersion != zone.Version {
		return dto.ZoneRevisionResponse{}, Conflict("version_conflict", "zone version changed; reload before editing the draft", repository.ErrVersionConflict)
	}
	draft, found, err := service.openDraft(zoneID)
	if err != nil {
		return dto.ZoneRevisionResponse{}, err
	}
	now := time.Now().UTC()
	apply := func(revision *model.ZoneRevision) {
		revision.RobotCellID = zone.RobotCellID
		revision.Name = strings.TrimSpace(request.Name)
		revision.ZoneType = request.ZoneType
		revision.PolygonGeoJSON = string(request.PolygonGeoJSON)
		revision.MinHeightMM = request.MinHeightMM
		revision.MaxHeightMM = request.MaxHeightMM
		revision.SpeedLimitMMS = request.SpeedLimitMMS
		revision.AccessRule = strings.TrimSpace(request.AccessRule)
		revision.BaseVersion = zone.Version
	}
	if found {
		apply(&draft)
		if err := service.revisions.Update(&draft); err != nil {
			if errors.Is(err, repository.ErrStateConflict) {
				return dto.ZoneRevisionResponse{}, Conflict("state_conflict", "the draft was published concurrently", err)
			}
			return dto.ZoneRevisionResponse{}, Internal("could not update revision draft", err)
		}
	} else {
		slot := 1
		draft = model.ZoneRevision{
			SafetyZoneID: zoneID, RevisionStatus: constants.RevisionStatusDraft, OpenDraftSlot: &slot,
			BaseVersion: zone.Version, CreatedBy: actor.ID, CreatedAt: now, UpdatedAt: now,
		}
		apply(&draft)
		if err := service.revisions.Create(&draft); err != nil {
			if repository.IsUniqueViolation(err) {
				return dto.ZoneRevisionResponse{}, Conflict("draft_exists", "an unpublished draft already exists for this zone", err)
			}
			return dto.ZoneRevisionResponse{}, Internal("could not create revision draft", err)
		}
	}
	if err := service.system.RecordAudit(actor, requestID, "zone_revision.draft_saved", "zone_revision", auditID(draft.ID), map[string]any{"safety_zone_id": zoneID, "base_version": zone.Version}, nil, revisionDraftSummary(draft)); err != nil {
		return dto.ZoneRevisionResponse{}, err
	}
	return service.Get(draft.ID)
}

func (service *ZoneRevisionService) OpenDraft(zoneID uint) (dto.ZoneRevisionResponse, error) {
	if _, err := service.zones.Get(zoneID); err != nil {
		return dto.ZoneRevisionResponse{}, MapRepositoryError("safety zone", err)
	}
	draft, _, err := service.openDraft(zoneID)
	if err != nil {
		return dto.ZoneRevisionResponse{}, err
	}
	return service.Get(draft.ID)
}

func (service *ZoneRevisionService) List(zoneID uint) ([]dto.ZoneRevisionResponse, error) {
	if zoneID == 0 {
		return nil, BadRequest("safety_zone_id_required", "safety_zone_id query parameter is required")
	}
	if _, err := service.zones.Get(zoneID); err != nil {
		return nil, MapRepositoryError("safety zone", err)
	}
	revisions, err := service.revisions.ListByZone(zoneID)
	if err != nil {
		return nil, Internal("could not list zone revisions", err)
	}
	responses := make([]dto.ZoneRevisionResponse, 0, len(revisions))
	for _, revision := range revisions {
		response, err := service.assemble(revision)
		if err != nil {
			return nil, err
		}
		responses = append(responses, response)
	}
	return responses, nil
}

func (service *ZoneRevisionService) Get(id uint) (dto.ZoneRevisionResponse, error) {
	revision, err := service.revisions.Get(id)
	if err != nil {
		return dto.ZoneRevisionResponse{}, MapRepositoryError("zone revision", err)
	}
	return service.assemble(revision)
}

// GetForZone resolves a revision by its own revision id while confirming it
// belongs to the zone in the path. The zone id must never be mistaken for the
// revision id.
func (service *ZoneRevisionService) GetForZone(zoneID, revisionID uint) (dto.ZoneRevisionResponse, error) {
	revision, err := service.revisions.GetForZone(zoneID, revisionID)
	if err != nil {
		return dto.ZoneRevisionResponse{}, MapRepositoryError("zone revision", err)
	}
	return service.assemble(revision)
}

// Publish is the sole finalization entry. The zone version bump, impact list
// and re-evaluation flags commit in one database transaction; any failure rolls
// the entire operation back.
func (service *ZoneRevisionService) Publish(zoneID uint, request dto.PublishZoneRevisionRequest, actor dto.Actor, requestID string) (dto.ZoneRevisionResponse, error) {
	zone, err := service.zones.Get(zoneID)
	if err != nil {
		return dto.ZoneRevisionResponse{}, MapRepositoryError("safety zone", err)
	}
	if zone.ZoneState == constants.ZoneStateInactive {
		return dto.ZoneRevisionResponse{}, Conflict("zone_inactive", "an inactive zone cannot publish a revision", repository.ErrStateConflict)
	}
	draft, found, err := service.openDraft(zoneID)
	if err != nil {
		return dto.ZoneRevisionResponse{}, err
	}
	if !found {
		return dto.ZoneRevisionResponse{}, Conflict("draft_missing", "no unpublished draft exists for this zone; create one before publishing", repository.ErrStateConflict)
	}
	if request.ExpectedVersion > 0 && request.ExpectedVersion != zone.Version {
		return dto.ZoneRevisionResponse{}, Conflict("version_conflict", "zone version changed; reload before publishing", repository.ErrVersionConflict)
	}
	if err := validateZone(draft.ZoneType, []byte(draft.PolygonGeoJSON), draft.MinHeightMM, draft.MaxHeightMM, draft.SpeedLimitMMS); err != nil {
		return dto.ZoneRevisionResponse{}, err
	}
	newVersion := zone.Version + 1
	err = service.db.Transaction(func(tx *gorm.DB) error {
		txRevisions := service.revisions.WithDB(tx)
		txPrograms := service.programs.WithDB(tx)
		txValidations := service.validations.WithDB(tx)

		if txErr := service.faultHook(stageLoadActivePrograms); txErr != nil {
			return txErr
		}
		activePrograms, txErr := txPrograms.ActiveForCell(zone.RobotCellID)
		if txErr != nil {
			return txErr
		}
		revisedVolume, txErr := revisedZoneVolume(draft)
		if txErr != nil {
			return Unprocessable("invalid_revision_geometry", txErr.Error(), txErr)
		}
		publishedAt := time.Now().UTC()
		impacts := make([]model.ZoneRevisionImpact, 0, len(activePrograms))
		affectedProgramIDs := make([]uint, 0, len(activePrograms))
		for _, program := range activePrograms {
			impact, txErr := evaluateProgramImpact(draft.ID, program, revisedVolume)
			if txErr != nil {
				return Unprocessable("invalid_active_program", txErr.Error(), txErr)
			}
			impacts = append(impacts, impact)
			if impact.Affected {
				affectedProgramIDs = append(affectedProgramIDs, program.ID)
			}
		}

		// Version and impact list are fixed together: the conditional zone
		// update and draft publish must both succeed in this transaction.
		if txErr := service.faultHook(stageApplyZoneVersion); txErr != nil {
			return txErr
		}
		if txErr := txRevisions.ApplyRevisionToZone(zoneID, zone.Version, newVersion, draft); txErr != nil {
			return txErr
		}
		if txErr := service.faultHook(stagePublishDraft); txErr != nil {
			return txErr
		}
		if txErr := txRevisions.Publish(draft.ID, draft.BaseVersion, newVersion, publishedAt); txErr != nil {
			return txErr
		}
		for index := range impacts {
			if txErr := txRevisions.CreateImpact(&impacts[index]); txErr != nil {
				return txErr
			}
			// Injectable failure point: the first impact row has already been
			// written in this transaction, so a rollback here must discard it.
			if index == 0 {
				if txErr := service.faultHook(stagePersistImpacts); txErr != nil {
					return txErr
				}
			}
		}

		acceptedRuns, txErr := txValidations.AcceptedForPrograms(affectedProgramIDs)
		if txErr != nil {
			return txErr
		}
		if txErr := service.faultHook(stagePersistReevalFlags); txErr != nil {
			return txErr
		}
		for _, run := range acceptedRuns {
			if _, lookupErr := txRevisions.ExistingReevaluationFlag(run.ID); lookupErr == nil {
				continue // already awaiting re-evaluation from an earlier revision; history stays untouched
			} else if !errors.Is(lookupErr, gorm.ErrRecordNotFound) && !strings.Contains(lookupErr.Error(), "record not found") {
				return lookupErr
			}
			flag := model.ValidationReevaluation{
				ZoneRevisionID: draft.ID, ValidationRunID: run.ID, MotionProgramID: run.MotionProgramID,
				PriorStatus: constants.ValidationAccepted,
				FlagReason:  fmt.Sprintf("Accepted offline validation predates published safety-zone revision v%d; re-evaluate against the revised zone.", newVersion),
				CreatedAt:   publishedAt,
			}
			if txErr := txRevisions.CreateReevaluation(&flag); txErr != nil {
				if repository.IsUniqueViolation(txErr) {
					continue
				}
				return txErr
			}
		}

		afterSummary := map[string]any{
			"zone_version": newVersion, "affected_active_programs": len(affectedProgramIDs),
			"accepted_validations_flagged": len(acceptedRuns),
		}
		if txErr := service.faultHook(stageRecordPublishAudit); txErr != nil {
			return txErr
		}
		return service.system.RecordAuditTx(tx, actor, requestID, "zone_revision.published", "zone_revision", auditID(draft.ID), map[string]any{"safety_zone_id": zoneID, "new_version": newVersion}, revisionDraftSummary(draft), afterSummary)
	})
	if err != nil {
		if errors.Is(err, repository.ErrVersionConflict) {
			return dto.ZoneRevisionResponse{}, Conflict("version_conflict", "zone version changed concurrently; the revision was not published", err)
		}
		if errors.Is(err, repository.ErrStateConflict) {
			return dto.ZoneRevisionResponse{}, Conflict("state_conflict", "the draft was published concurrently", err)
		}
		var appError *AppError
		if errors.As(err, &appError) {
			return dto.ZoneRevisionResponse{}, appError
		}
		var injected *revisionPublishError
		if errors.As(err, &injected) {
			return dto.ZoneRevisionResponse{}, Internal("zone revision publish failed at stage "+injected.Stage()+"; all changes were rolled back", err)
		}
		return dto.ZoneRevisionResponse{}, Internal("could not publish zone revision", err)
	}
	return service.Get(draft.ID)
}

func (service *ZoneRevisionService) openDraft(zoneID uint) (model.ZoneRevision, bool, error) {
	draft, err := service.revisions.OpenDraft(zoneID)
	if err == nil {
		return draft, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) || strings.Contains(err.Error(), "record not found") {
		return model.ZoneRevision{}, false, nil
	}
	return model.ZoneRevision{}, false, Internal("could not load revision draft", err)
}

func (service *ZoneRevisionService) assemble(revision model.ZoneRevision) (dto.ZoneRevisionResponse, error) {
	impacts, err := service.revisions.Impacts(revision.ID)
	if err != nil {
		return dto.ZoneRevisionResponse{}, Internal("could not load revision impacts", err)
	}
	flags, err := service.revisions.ReevaluationFlags(revision.ID)
	if err != nil {
		return dto.ZoneRevisionResponse{}, Internal("could not load re-evaluation flags", err)
	}
	response := dto.ZoneRevisionResponse{
		ID: revision.ID, SafetyZoneID: revision.SafetyZoneID, RobotCellID: revision.RobotCellID,
		RobotCellCode: revision.SafetyZone.RobotCell.CellCode, ZoneName: revision.SafetyZone.Name,
		RevisionStatus: revision.RevisionStatus, BaseVersion: revision.BaseVersion,
		PublishedVersion: revision.PublishedVersion, Name: revision.Name, ZoneType: revision.ZoneType,
		PolygonGeoJSON: json.RawMessage(revision.PolygonGeoJSON), MinHeightMM: revision.MinHeightMM,
		MaxHeightMM: revision.MaxHeightMM, SpeedLimitMMS: revision.SpeedLimitMMS, AccessRule: revision.AccessRule,
		CreatedAt: revision.CreatedAt, UpdatedAt: revision.UpdatedAt, PublishedAt: revision.PublishedAt,
		Impacts:           make([]dto.ZoneRevisionImpactItem, 0, len(impacts)),
		ReevaluationFlags: make([]dto.ReevaluationFlagItem, 0, len(flags)),
	}
	for _, impact := range impacts {
		response.Impacts = append(response.Impacts, dto.ZoneRevisionImpactItem{
			MotionProgramID: impact.MotionProgramID, ProgramCode: impact.ProgramCode, ProgramVersion: impact.ProgramVersion,
			ProgramState: impact.ProgramState, Affected: impact.Affected, CollisionCount: impact.CollisionCount,
			FirstSegment: impact.FirstSegment, FirstTimeMS: impact.FirstTimeMS, MinClearanceMM: impact.MinClearanceMM,
			ActualSpeedMMS: impact.ActualSpeedMMS, AllowedSpeedMMS: impact.AllowedSpeedMMS, Evidence: impact.Evidence,
		})
	}
	for _, flag := range flags {
		response.ReevaluationFlags = append(response.ReevaluationFlags, dto.ReevaluationFlagItem{
			ValidationRunID: flag.ValidationRunID, MotionProgramID: flag.MotionProgramID, PriorStatus: flag.PriorStatus,
			FlagReason: flag.FlagReason, CreatedAt: flag.CreatedAt,
		})
	}
	return response, nil
}

func revisedZoneVolume(revision model.ZoneRevision) (geometry.ZoneVolume, error) {
	polygon, err := geometry.ParsePolygon([]byte(revision.PolygonGeoJSON))
	if err != nil {
		return geometry.ZoneVolume{}, err
	}
	return geometry.ZoneVolume{
		ID: revision.SafetyZoneID, Name: revision.Name, ZoneType: revision.ZoneType, Polygon: polygon,
		MinHeightMM: revision.MinHeightMM, MaxHeightMM: revision.MaxHeightMM, SpeedLimitMMS: revision.SpeedLimitMMS,
	}, nil
}

func evaluateProgramImpact(revisionID uint, program model.MotionProgram, revisedVolume geometry.ZoneVolume) (model.ZoneRevisionImpact, error) {
	var trajectory []dto.TrajectoryPoint
	if err := json.Unmarshal([]byte(program.TrajectoryJSON), &trajectory); err != nil {
		return model.ZoneRevisionImpact{}, fmt.Errorf("program %s trajectory: %w", program.ProgramCode, err)
	}
	if err := geometry.ValidateTrajectory(trajectory); err != nil {
		return model.ZoneRevisionImpact{}, fmt.Errorf("program %s trajectory: %w", program.ProgramCode, err)
	}
	events := geometry.EvaluateEnvelope(trajectory, program.ToolRadiusMM+program.PayloadRadiusMM, []geometry.ZoneVolume{revisedVolume})
	impact := model.ZoneRevisionImpact{
		ZoneRevisionID: revisionID, MotionProgramID: program.ID, ProgramCode: program.ProgramCode,
		ProgramVersion: program.Version, ProgramState: program.ProgramState, Affected: len(events) > 0,
		FirstSegment: -1, FirstTimeMS: -1, MinClearanceMM: 0, CreatedAt: time.Now().UTC(),
		Evidence: "expanded envelope stays clear of the revised zone volume",
	}
	if len(events) == 0 {
		return impact, nil
	}
	closest := events[0]
	for _, event := range events[1:] {
		if event.ClearanceMM < closest.ClearanceMM {
			closest = event
		}
	}
	impact.CollisionCount = len(events)
	impact.FirstSegment = closest.SegmentIndex
	impact.FirstTimeMS = closest.FirstTimeMS
	impact.MinClearanceMM = math.Round(closest.ClearanceMM*100) / 100
	impact.ActualSpeedMMS = closest.ActualSpeedMMS
	impact.AllowedSpeedMMS = closest.AllowedSpeedMMS
	impact.Evidence = closest.Evidence
	return impact, nil
}

func revisionDraftSummary(revision model.ZoneRevision) map[string]any {
	return map[string]any{
		"safety_zone_id": revision.SafetyZoneID, "revision_status": revision.RevisionStatus,
		"base_version": revision.BaseVersion, "zone_type": revision.ZoneType, "name": revision.Name,
	}
}
