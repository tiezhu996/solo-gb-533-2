package service

import (
	"errors"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"robot-cell-safety-envelope-validator/backend/internal/constants"
	"robot-cell-safety-envelope-validator/backend/internal/dto"
	"robot-cell-safety-envelope-validator/backend/internal/model"
	"robot-cell-safety-envelope-validator/backend/internal/repository"
)

type revisionFixture struct {
	db          *gorm.DB
	service     *ZoneRevisionService
	zoneService *SafetyZoneService
	zones       *repository.SafetyZoneRepository
	validations *repository.ValidationRunRepository
	zone        model.SafetyZone
	programA    model.MotionProgram
	programB    model.MotionProgram
	accepted    model.ValidationRun
}

func polygonSquare() string {
	return `{"type":"Polygon","coordinates":[[[-900,-900],[900,-900],[900,900],[-900,900],[-900,-900]]]}`
}

func polygonGate() string {
	return `{"type":"Polygon","coordinates":[[[1050,-500],[1750,-500],[1750,500],[1050,500],[1050,-500]]]}`
}

func newRevisionFixture(t *testing.T) revisionFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:revision-"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.RobotCell{}, &model.SafetyZone{}, &model.MotionProgram{},
		&model.ValidationRun{}, &model.AuditEvent{}, &model.ZoneRevision{},
		&model.ZoneRevisionImpact{}, &model.ValidationReevaluation{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	systemRepo := repository.NewSystemRepository(db)
	cellRepo := repository.NewRobotCellRepository(db)
	zoneRepo := repository.NewSafetyZoneRepository(db)
	revisionRepo := repository.NewZoneRevisionRepository(db)
	programRepo := repository.NewMotionProgramRepository(db)
	validationRepo := repository.NewValidationRunRepository(db)
	systemService := NewSystemService(systemRepo, "revision-unit-test-secret-0123456789", time.Hour)
	svc := NewZoneRevisionService(db, revisionRepo, zoneRepo, programRepo, validationRepo, systemService)
	zoneSvc := NewSafetyZoneService(zoneRepo, revisionRepo, cellRepo, systemService)

	now := time.Now().UTC()
	cell := model.RobotCell{
		CellCode: "CELL-R1", Name: "Revision cell", LayoutGeoJSON: `{"type":"FeatureCollection","features":[]}`,
		RobotModel: "R", ControllerModel: "C", MaxReachMM: 2700, OwnerTeam: "Team",
		CellState: constants.CellStateFrozen, LayoutVersion: 1, CreatedBy: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&cell).Error; err != nil {
		t.Fatalf("create cell: %v", err)
	}
	zone := model.SafetyZone{
		RobotCellID: cell.ID, Name: "Operating envelope", ZoneType: constants.ZoneTypeOperating,
		PolygonGeoJSON: polygonSquare(), MinHeightMM: 0, MaxHeightMM: 2200, SpeedLimitMMS: 1500,
		AccessRule: "guards closed", ZoneState: constants.ZoneStateActive, Version: 2,
		CreatedBy: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&zone).Error; err != nil {
		t.Fatalf("create zone: %v", err)
	}
	trajectoryA := `[{"x_mm":-650,"y_mm":0,"z_mm":750,"time_ms":0,"speed_mm_s":420},{"x_mm":300,"y_mm":120,"z_mm":820,"time_ms":2200,"speed_mm_s":460},{"x_mm":1280,"y_mm":80,"z_mm":900,"time_ms":4300,"speed_mm_s":470}]`
	trajectoryB := `[{"x_mm":0,"y_mm":0,"z_mm":750,"time_ms":0,"speed_mm_s":300},{"x_mm":100,"y_mm":0,"z_mm":750,"time_ms":1000,"speed_mm_s":300}]`
	interlocks := `[{"name":"estop_reset","sequence":1,"depends_on":[]}]`
	programA := model.MotionProgram{
		RobotCellID: cell.ID, ProgramCode: "ENTER-GATE", Version: 1, TrajectoryJSON: trajectoryA,
		ToolRadiusMM: 180, PayloadRadiusMM: 120, InterlockSequenceJSON: interlocks,
		SourceChecksum: "aaaa", ProgramState: constants.ProgramStateActive, UploadedBy: 2, UploadedAt: now, UpdatedAt: now,
	}
	programB := model.MotionProgram{
		RobotCellID: cell.ID, ProgramCode: "STAY-HOME", Version: 1, TrajectoryJSON: trajectoryB,
		ToolRadiusMM: 25, PayloadRadiusMM: 25, InterlockSequenceJSON: interlocks,
		SourceChecksum: "bbbb", ProgramState: constants.ProgramStateActive, UploadedBy: 2, UploadedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&programA).Error; err != nil {
		t.Fatalf("create program A: %v", err)
	}
	if err := db.Create(&programB).Error; err != nil {
		t.Fatalf("create program B: %v", err)
	}
	accepted := model.ValidationRun{
		MotionProgramID: programA.ID, ZoneSnapshot: `[{"id":1}]`, ProgramSnapshot: `{"program":"ENTER-GATE"}`,
		AlgorithmVersion: "envelope-2d-height-v1.0", InputHash: "hash-accepted", IdempotencyKey: "accepted-key-0001",
		CollisionEventsJSON:   `[{"segment_index":1,"zone_name":"Operating envelope","violation":true,"evidence":"historical evidence must survive"}]`,
		InterlockFindingsJSON: `[]`, RiskScore: 44, ValidationStatus: constants.ValidationAccepted,
		Explanation: "accepted offline evidence", RequestedBy: 1, StartedAt: now.Add(-time.Hour),
	}
	if err := db.Create(&accepted).Error; err != nil {
		t.Fatalf("create accepted run: %v", err)
	}
	return revisionFixture{
		db: db, service: svc, zoneService: zoneSvc, zones: zoneRepo, validations: validationRepo,
		zone: zone, programA: programA, programB: programB, accepted: accepted,
	}
}

func gateDraftRequest() dto.SaveZoneRevisionDraftRequest {
	return dto.SaveZoneRevisionDraftRequest{
		Name: "Operator transfer gate", ZoneType: constants.ZoneTypeRestricted,
		PolygonGeoJSON: []byte(polygonGate()), MinHeightMM: 0, MaxHeightMM: 2400,
		SpeedLimitMMS: 250, AccessRule: "gate lock and light curtain clear",
	}
}

func TestSingleOpenDraftPerZone(t *testing.T) {
	fixture := newRevisionFixture(t)
	actor := dto.Actor{ID: 1, Username: "engineer", Role: constants.RoleSafetyEngineer}

	first, err := fixture.service.SaveDraft(fixture.zone.ID, gateDraftRequest(), actor, "req-1")
	if err != nil {
		t.Fatalf("save first draft: %v", err)
	}
	second, err := fixture.service.SaveDraft(fixture.zone.ID, gateDraftRequest(), actor, "req-2")
	if err != nil {
		t.Fatalf("resave draft: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected the same open draft to be updated, got ids %d and %d", first.ID, second.ID)
	}
	var draftCount int64
	if err := fixture.db.Model(&model.ZoneRevision{}).Where("safety_zone_id = ? AND revision_status = ?", fixture.zone.ID, "draft").Count(&draftCount).Error; err != nil {
		t.Fatalf("count drafts: %v", err)
	}
	if draftCount != 1 {
		t.Fatalf("expected exactly one open draft, got %d", draftCount)
	}
}

func TestPublishFixesVersionImpactAndFlagsAtomically(t *testing.T) {
	fixture := newRevisionFixture(t)
	actor := dto.Actor{ID: 1, Username: "engineer", Role: constants.RoleSafetyEngineer}

	if _, err := fixture.service.SaveDraft(fixture.zone.ID, gateDraftRequest(), actor, "req-1"); err != nil {
		t.Fatalf("save draft: %v", err)
	}
	published, err := fixture.service.Publish(fixture.zone.ID, dto.PublishZoneRevisionRequest{}, actor, "req-2")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if published.RevisionStatus != constants.RevisionStatusPublished {
		t.Fatalf("revision status = %s, want published", published.RevisionStatus)
	}
	if published.PublishedVersion == nil || *published.PublishedVersion != fixture.zone.Version+1 {
		t.Fatalf("published version = %v, want %d", published.PublishedVersion, fixture.zone.Version+1)
	}
	liveZone, err := fixture.zones.Get(fixture.zone.ID)
	if err != nil {
		t.Fatalf("reload zone: %v", err)
	}
	if liveZone.Version != fixture.zone.Version+1 {
		t.Fatalf("live zone version = %d, want %d", liveZone.Version, fixture.zone.Version+1)
	}
	if liveZone.ZoneType != constants.ZoneTypeRestricted || liveZone.Name != "Operator transfer gate" {
		t.Fatalf("live zone was not updated to revised definition: %+v", liveZone)
	}
	if len(published.Impacts) != 2 {
		t.Fatalf("expected impacts for 2 active programs, got %d", len(published.Impacts))
	}
	byProgram := map[uint]dto.ZoneRevisionImpactItem{}
	for _, impact := range published.Impacts {
		byProgram[impact.MotionProgramID] = impact
	}
	impactA := byProgram[fixture.programA.ID]
	if !impactA.Affected || impactA.CollisionCount == 0 {
		t.Fatalf("program ENTER-GATE should be affected by the revised gate, got %+v", impactA)
	}
	if byProgram[fixture.programB.ID].Affected {
		t.Fatalf("program STAY-HOME should remain clear of the revised gate")
	}
	if len(published.ReevaluationFlags) != 1 {
		t.Fatalf("expected exactly one re-evaluation flag, got %d", len(published.ReevaluationFlags))
	}
	flag := published.ReevaluationFlags[0]
	if flag.ValidationRunID != fixture.accepted.ID || flag.PriorStatus != constants.ValidationAccepted {
		t.Fatalf("unexpected re-evaluation flag: %+v", flag)
	}

	// Historical evidence is untouched and remains readable.
	reloadedRun, err := fixture.validations.Get(fixture.accepted.ID)
	if err != nil {
		t.Fatalf("read accepted validation: %v", err)
	}
	if reloadedRun.ValidationStatus != constants.ValidationAccepted {
		t.Fatalf("accepted validation status changed to %s; it must only be flagged", reloadedRun.ValidationStatus)
	}
	if reloadedRun.CollisionEventsJSON != fixture.accepted.CollisionEventsJSON {
		t.Fatalf("historical collision evidence was modified:\nbefore=%s\nafter=%s", fixture.accepted.CollisionEventsJSON, reloadedRun.CollisionEventsJSON)
	}
	if reloadedRun.RiskScore != fixture.accepted.RiskScore {
		t.Fatalf("historical risk score changed from %v to %v", fixture.accepted.RiskScore, reloadedRun.RiskScore)
	}

	// After publishing, a new draft may be opened; republishing without one fails.
	if _, err := fixture.service.Publish(fixture.zone.ID, dto.PublishZoneRevisionRequest{}, actor, "req-3"); err == nil {
		t.Fatal("expected publishing again with no open draft to fail")
	}
	if _, err := fixture.service.SaveDraft(fixture.zone.ID, gateDraftRequest(), actor, "req-4"); err != nil {
		t.Fatalf("open a fresh draft after publish: %v", err)
	}
}

func TestPublishFailureRollsBackEverything(t *testing.T) {
	fixture := newRevisionFixture(t)
	actor := dto.Actor{ID: 1, Username: "engineer", Role: constants.RoleSafetyEngineer}

	if _, err := fixture.service.SaveDraft(fixture.zone.ID, gateDraftRequest(), actor, "req-1"); err != nil {
		t.Fatalf("save draft: %v", err)
	}
	// Introduce an active program whose stored trajectory is invalid; impact
	// evaluation fails mid-transaction and must roll everything back.
	bad := model.MotionProgram{
		RobotCellID: fixture.zone.RobotCellID, ProgramCode: "BROKEN-TRAJ", Version: 1,
		TrajectoryJSON: `[{"x_mm":0,"y_mm":0,"z_mm":0,"time_ms":0}]`, // single point -> invalid
		ToolRadiusMM:   10, PayloadRadiusMM: 10,
		InterlockSequenceJSON: `[{"name":"estop_reset","sequence":1,"depends_on":[]}]`,
		SourceChecksum:        "cccc", ProgramState: constants.ProgramStateActive, UploadedBy: 2,
		UploadedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := fixture.db.Create(&bad).Error; err != nil {
		t.Fatalf("create broken program: %v", err)
	}
	if _, err := fixture.service.Publish(fixture.zone.ID, dto.PublishZoneRevisionRequest{}, actor, "req-2"); err == nil {
		t.Fatal("expected publish to fail on the invalid active program")
	}

	liveZone, err := fixture.zones.Get(fixture.zone.ID)
	if err != nil {
		t.Fatalf("reload zone: %v", err)
	}
	if liveZone.Version != fixture.zone.Version {
		t.Fatalf("zone version changed to %d after failed publish; rollback expected", liveZone.Version)
	}
	if liveZone.ZoneType != constants.ZoneTypeOperating {
		t.Fatalf("zone definition changed after failed publish")
	}
	var openDrafts int64
	if err := fixture.db.Model(&model.ZoneRevision{}).Where("safety_zone_id = ? AND revision_status = ?", fixture.zone.ID, "draft").Count(&openDrafts).Error; err != nil {
		t.Fatalf("count drafts: %v", err)
	}
	if openDrafts != 1 {
		t.Fatalf("draft should remain open after rollback, got %d", openDrafts)
	}
	var impactCount, flagCount int64
	if err := fixture.db.Model(&model.ZoneRevisionImpact{}).Count(&impactCount).Error; err != nil {
		t.Fatalf("count impacts: %v", err)
	}
	if err := fixture.db.Model(&model.ValidationReevaluation{}).Count(&flagCount).Error; err != nil {
		t.Fatalf("count flags: %v", err)
	}
	if impactCount != 0 || flagCount != 0 {
		t.Fatalf("rollback left %d impacts and %d flags", impactCount, flagCount)
	}
}

func TestDirectZoneUpdateRejectedWhileDraftOpen(t *testing.T) {
	fixture := newRevisionFixture(t)
	actor := dto.Actor{ID: 1, Username: "engineer", Role: constants.RoleSafetyEngineer}

	draft, err := fixture.service.SaveDraft(fixture.zone.ID, gateDraftRequest(), actor, "req-1")
	if err != nil {
		t.Fatalf("save draft: %v", err)
	}
	direct := dto.UpdateSafetyZoneRequest{
		Name: "Hijacked by direct PUT", ZoneType: constants.ZoneTypeRestricted,
		PolygonGeoJSON: []byte(polygonGate()), MinHeightMM: 0, MaxHeightMM: 2400,
		SpeedLimitMMS: 90, AccessRule: "bypassed revision entry", Version: fixture.zone.Version,
	}
	if _, err := fixture.zoneService.Update(fixture.zone.ID, direct, actor, "req-direct"); err == nil {
		t.Fatal("expected direct zone update to be rejected while a revision draft is open")
	} else {
		var appError *AppError
		if !errors.As(err, &appError) || appError.Code != "revision_draft_open" {
			t.Fatalf("expected revision_draft_open conflict, got %v", err)
		}
	}

	// Live zone unchanged.
	liveZone, err := fixture.zones.Get(fixture.zone.ID)
	if err != nil {
		t.Fatalf("reload zone: %v", err)
	}
	if liveZone.Version != fixture.zone.Version {
		t.Fatalf("live zone version changed to %d; must stay %d", liveZone.Version, fixture.zone.Version)
	}
	if liveZone.Name != fixture.zone.Name || liveZone.ZoneType != fixture.zone.ZoneType {
		t.Fatalf("live zone definition was modified by the rejected direct update")
	}

	// Open draft unchanged.
	openDraft, err := fixture.service.Get(draft.ID)
	if err != nil {
		t.Fatalf("read draft: %v", err)
	}
	if openDraft.RevisionStatus != constants.RevisionStatusDraft || openDraft.Name != gateDraftRequest().Name {
		t.Fatalf("open draft was modified: %+v", openDraft)
	}

	// No published revision must exist for the zone.
	var publishedCount int64
	if err := fixture.db.Model(&model.ZoneRevision{}).
		Where("safety_zone_id = ? AND revision_status = ?", fixture.zone.ID, constants.RevisionStatusPublished).
		Count(&publishedCount).Error; err != nil {
		t.Fatalf("count published: %v", err)
	}
	if publishedCount != 0 {
		t.Fatalf("expected no published revisions, got %d", publishedCount)
	}
}

func TestGetForZoneReadsByRevisionID(t *testing.T) {
	fixture := newRevisionFixture(t)
	actor := dto.Actor{ID: 1, Username: "engineer", Role: constants.RoleSafetyEngineer}

	// A second zone makes its zone id differ from the first revision's own id,
	// which is the case that exposed the zone-id/revision-id confusion.
	secondZone := model.SafetyZone{
		RobotCellID: fixture.zone.RobotCellID, Name: "Second envelope", ZoneType: constants.ZoneTypeService,
		PolygonGeoJSON: polygonSquare(), MinHeightMM: 0, MaxHeightMM: 2000, SpeedLimitMMS: 0,
		AccessRule: "service only", ZoneState: constants.ZoneStateActive, Version: 1,
		CreatedBy: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := fixture.db.Create(&secondZone).Error; err != nil {
		t.Fatalf("create second zone: %v", err)
	}

	request := dto.SaveZoneRevisionDraftRequest{
		Name: "Second envelope revised", ZoneType: constants.ZoneTypeService,
		PolygonGeoJSON: []byte(polygonGate()), MinHeightMM: 0, MaxHeightMM: 2000,
		SpeedLimitMMS: 0, AccessRule: "service only revised",
	}
	draft, err := fixture.service.SaveDraft(secondZone.ID, request, actor, "req-1")
	if err != nil {
		t.Fatalf("save draft on second zone: %v", err)
	}
	if draft.ID == secondZone.ID {
		t.Fatalf("revision id %d must differ from zone id %d for this test", draft.ID, secondZone.ID)
	}

	// Reading with the revision's own id, scoped to its zone, works.
	byRevisionID, err := fixture.service.GetForZone(secondZone.ID, draft.ID)
	if err != nil {
		t.Fatalf("get revision by its own id: %v", err)
	}
	if byRevisionID.ID != draft.ID || byRevisionID.SafetyZoneID != secondZone.ID {
		t.Fatalf("returned wrong revision: %+v", byRevisionID)
	}

	// Passing the zone id where the revision id belongs must not return it.
	if _, err := fixture.service.GetForZone(secondZone.ID, secondZone.ID); err == nil {
		t.Fatal("using the zone id as the revision id must not return a revision")
	}

	// The revision is not readable through the other zone even though its
	// revision id is valid globally.
	if _, err := fixture.service.GetForZone(fixture.zone.ID, draft.ID); err == nil {
		t.Fatal("revision must not be readable scoped to a different zone")
	}

	// After publish the same revision id reads back as published.
	published, err := fixture.service.Publish(secondZone.ID, dto.PublishZoneRevisionRequest{}, actor, "req-2")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	reRead, err := fixture.service.GetForZone(secondZone.ID, published.ID)
	if err != nil {
		t.Fatalf("re-read published revision: %v", err)
	}
	if reRead.RevisionStatus != constants.RevisionStatusPublished || reRead.PublishedVersion == nil {
		t.Fatalf("published revision read back incorrectly: %+v", reRead)
	}
}
