package service

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"robot-cell-safety-envelope-validator/backend/internal/constants"
	"robot-cell-safety-envelope-validator/backend/internal/dto"
	"robot-cell-safety-envelope-validator/backend/internal/model"
	"robot-cell-safety-envelope-validator/backend/internal/repository"
)

// persistentRevisionStack is a real, file-backed SQLite stack. It uses WAL
// journaling, a multi-connection pool, IMMEDIATE write transactions and a busy
// timeout so two goroutines genuinely contend on the same rows; there are no
// in-memory doubles and the test never serializes the publishes.
type persistentRevisionStack struct {
	db      *gorm.DB
	path    string
	service *ZoneRevisionService
	zone    *repository.SafetyZoneRepository
}

func persistentDSN(path string) string {
	return path + "?_journal_mode=WAL&_busy_timeout=10000&_txlock=immediate"
}

// buildStack opens the file-backed database and wires the service. The revision
// store factory is the only test seam: the concurrency test passes the real
// repository straight through, while the fault test wraps that repository.
func buildStack(t *testing.T, path string, storeFactory func(db *gorm.DB, real repository.RevisionStore) repository.RevisionStore) (*gorm.DB, string, *ZoneRevisionService) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(persistentDSN(path)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open file sqlite: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		// More than one pooled connection is required for real concurrency.
		sqlDB.SetMaxOpenConns(4)
		sqlDB.SetMaxIdleConns(4)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.RobotCell{}, &model.SafetyZone{}, &model.MotionProgram{},
		&model.ValidationRun{}, &model.AuditEvent{}, &model.ZoneRevision{},
		&model.ZoneRevisionImpact{}, &model.ValidationReevaluation{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	systemRepo := repository.NewSystemRepository(db)
	zoneRepo := repository.NewSafetyZoneRepository(db)
	realRevisionRepo := repository.NewZoneRevisionRepository(db)
	var revisionStore repository.RevisionStore = realRevisionRepo
	if storeFactory != nil {
		revisionStore = storeFactory(db, realRevisionRepo)
	}
	programRepo := repository.NewMotionProgramRepository(db)
	validationRepo := repository.NewValidationRunRepository(db)
	systemService := NewSystemService(systemRepo, "revision-concurrency-secret-0123456789", time.Hour)
	svc := NewZoneRevisionService(db, revisionStore, zoneRepo, programRepo, validationRepo, systemService)
	return db, path, svc
}

func openPersistentStack(t *testing.T) persistentRevisionStack {
	t.Helper()
	path := filepath.Join(t.TempDir(), "revision-concurrency.db")
	db, path, svc := buildStack(t, path, nil)
	zoneRepo := repository.NewSafetyZoneRepository(db)
	return persistentRevisionStack{db: db, path: path, service: svc, zone: zoneRepo}
}

func (stack *persistentRevisionStack) close(t *testing.T) {
	t.Helper()
	if sqlDB, err := stack.db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}

// reopen simulates a service restart against the same on-disk database: a new
// connection pool and repositories, with no in-memory state carried over.
func (stack *persistentRevisionStack) reopen(t *testing.T) persistentRevisionStack {
	t.Helper()
	stack.close(t)
	db, path, svc := buildStack(t, stack.path, nil)
	zoneRepo := repository.NewSafetyZoneRepository(db)
	return persistentRevisionStack{db: db, path: path, service: svc, zone: zoneRepo}
}

// faultingRevisionStore is a test-only wrapper around the real revision
// repository. It fails the impact-persistence stage after the first impact row
// has been written in a transaction, so the rollback demonstrably discards a
// real partial write. Every other call delegates to the real repository.
type faultingRevisionStore struct {
	real repository.RevisionStore
}

func (store *faultingRevisionStore) Create(revision *model.ZoneRevision) error {
	return store.real.Create(revision)
}
func (store *faultingRevisionStore) Update(revision *model.ZoneRevision) error {
	return store.real.Update(revision)
}
func (store *faultingRevisionStore) OpenDraft(zoneID uint) (model.ZoneRevision, error) {
	return store.real.OpenDraft(zoneID)
}
func (store *faultingRevisionStore) Get(id uint) (model.ZoneRevision, error) {
	return store.real.Get(id)
}
func (store *faultingRevisionStore) GetForZone(zoneID, revisionID uint) (model.ZoneRevision, error) {
	return store.real.GetForZone(zoneID, revisionID)
}
func (store *faultingRevisionStore) ListByZone(zoneID uint) ([]model.ZoneRevision, error) {
	return store.real.ListByZone(zoneID)
}
func (store *faultingRevisionStore) Impacts(revisionID uint) ([]model.ZoneRevisionImpact, error) {
	return store.real.Impacts(revisionID)
}
func (store *faultingRevisionStore) ReevaluationFlags(revisionID uint) ([]model.ValidationReevaluation, error) {
	return store.real.ReevaluationFlags(revisionID)
}
func (store *faultingRevisionStore) WithDB(tx *gorm.DB) repository.RevisionTxStore {
	return &faultingRevisionTxStore{real: store.real.WithDB(tx)}
}

type faultingRevisionTxStore struct {
	real repository.RevisionTxStore
}

func (store *faultingRevisionTxStore) ApplyRevisionToZone(zoneID uint, expectedVersion, newVersion int, revision model.ZoneRevision) error {
	return store.real.ApplyRevisionToZone(zoneID, expectedVersion, newVersion, revision)
}
func (store *faultingRevisionTxStore) Publish(id uint, baseVersion, publishedVersion int, publishedAt any) error {
	return store.real.Publish(id, baseVersion, publishedVersion, publishedAt)
}
func (store *faultingRevisionTxStore) CreateImpact(impact *model.ZoneRevisionImpact) error {
	if err := store.real.CreateImpact(impact); err != nil {
		return err
	}
	// Fail the impact-persistence stage after the first impact row is written.
	return &faultStageError{stage: stagePersistImpacts}
}
func (store *faultingRevisionTxStore) CreateReevaluation(flag *model.ValidationReevaluation) error {
	return store.real.CreateReevaluation(flag)
}
func (store *faultingRevisionTxStore) ExistingReevaluationFlag(validationRunID uint) (model.ValidationReevaluation, error) {
	return store.real.ExistingReevaluationFlag(validationRunID)
}

// stagePersistImpacts names the single faulted publish stage; it is defined
// only in the test assembly, never in production code.
const stagePersistImpacts = "persist_impacts"

type faultStageError struct{ stage string }

func (err *faultStageError) Error() string { return "injected failure at publish stage: " + err.stage }
func (err *faultStageError) Stage() string { return err.stage }

// openFaultStack wires the revision service with the faulting revision store.
func openFaultStack(t *testing.T) persistentRevisionStack {
	t.Helper()
	path := filepath.Join(t.TempDir(), "revision-fault.db")
	factory := func(_ *gorm.DB, real repository.RevisionStore) repository.RevisionStore {
		return &faultingRevisionStore{real: real}
	}
	db, path, svc := buildStack(t, path, factory)
	zoneRepo := repository.NewSafetyZoneRepository(db)
	return persistentRevisionStack{db: db, path: path, service: svc, zone: zoneRepo}
}

type persistentFixture struct {
	zone     model.SafetyZone
	program  model.MotionProgram
	accepted model.ValidationRun
	actor    dto.Actor
}

func seedPersistentFixture(t *testing.T, stack persistentRevisionStack) persistentFixture {
	t.Helper()
	now := time.Now().UTC()
	cell := model.RobotCell{
		CellCode: "CELL-CONC", Name: "Concurrency cell", LayoutGeoJSON: `{"type":"FeatureCollection","features":[]}`,
		RobotModel: "R", ControllerModel: "C", MaxReachMM: 2700, OwnerTeam: "Team",
		CellState: constants.CellStateFrozen, LayoutVersion: 1, CreatedBy: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := stack.db.Create(&cell).Error; err != nil {
		t.Fatalf("create cell: %v", err)
	}
	zone := model.SafetyZone{
		RobotCellID: cell.ID, Name: "Operating envelope", ZoneType: constants.ZoneTypeOperating,
		PolygonGeoJSON: polygonSquare(), MinHeightMM: 0, MaxHeightMM: 2200, SpeedLimitMMS: 1500,
		AccessRule: "guards closed", ZoneState: constants.ZoneStateActive, Version: 2,
		CreatedBy: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := stack.db.Create(&zone).Error; err != nil {
		t.Fatalf("create zone: %v", err)
	}
	// Trajectory crosses the revised gate region, so the active program is affected.
	trajectory := `[{"x_mm":-650,"y_mm":0,"z_mm":750,"time_ms":0,"speed_mm_s":420},{"x_mm":1280,"y_mm":80,"z_mm":900,"time_ms":4300,"speed_mm_s":470}]`
	interlocks := `[{"name":"estop_reset","sequence":1,"depends_on":[]}]`
	program := model.MotionProgram{
		RobotCellID: cell.ID, ProgramCode: "ENTER-GATE", Version: 1, TrajectoryJSON: trajectory,
		ToolRadiusMM: 180, PayloadRadiusMM: 120, InterlockSequenceJSON: interlocks,
		SourceChecksum: "aaaa", ProgramState: constants.ProgramStateActive, UploadedBy: 2, UploadedAt: now, UpdatedAt: now,
	}
	if err := stack.db.Create(&program).Error; err != nil {
		t.Fatalf("create program: %v", err)
	}
	accepted := model.ValidationRun{
		MotionProgramID: program.ID, ZoneSnapshot: `[{"id":1}]`, ProgramSnapshot: `{"program":"ENTER-GATE"}`,
		AlgorithmVersion: "envelope-2d-height-v1.0", InputHash: "hash-accepted-conc",
		IdempotencyKey:        "accepted-key-conc-0001",
		CollisionEventsJSON:   `[{"segment_index":0,"zone_name":"Operating envelope","violation":true,"evidence":"persisted evidence must survive rollback and restart"}]`,
		InterlockFindingsJSON: `[]`, RiskScore: 44, ValidationStatus: constants.ValidationAccepted,
		Explanation: "accepted offline evidence", RequestedBy: 1, StartedAt: now.Add(-time.Hour),
	}
	if err := stack.db.Create(&accepted).Error; err != nil {
		t.Fatalf("create accepted run: %v", err)
	}
	actor := dto.Actor{ID: 1, Username: "engineer", Role: constants.RoleSafetyEngineer}
	if _, err := stack.service.SaveDraft(zone.ID, gateDraftRequest(), actor, "seed-draft"); err != nil {
		t.Fatalf("seed draft: %v", err)
	}
	return persistentFixture{zone: zone, program: program, accepted: accepted, actor: actor}
}

// TestConcurrentPublishSingleWinner fires two genuinely concurrent publishes
// of the same open draft. Exactly one may advance the zone version; the other
// must receive a retryable version/state conflict rather than a second version.
func TestConcurrentPublishSingleWinner(t *testing.T) {
	stack := openPersistentStack(t)
	defer stack.close(t)
	fixture := seedPersistentFixture(t, stack)

	start := make(chan struct{})
	results := make([]error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, results[0] = stack.service.Publish(fixture.zone.ID, dto.PublishZoneRevisionRequest{}, fixture.actor, "concurrent-1")
	}()
	go func() {
		defer wg.Done()
		<-start
		_, results[1] = stack.service.Publish(fixture.zone.ID, dto.PublishZoneRevisionRequest{}, fixture.actor, "concurrent-2")
	}()
	close(start)
	wg.Wait()

	okCount, conflictCount := 0, 0
	for index, err := range results {
		switch {
		case err == nil:
			okCount++
		case isRetryablePublishConflict(err):
			conflictCount++
		default:
			t.Fatalf("publish %d returned an unexpected error: %v", index, err)
		}
	}
	if okCount != 1 || conflictCount != 1 {
		t.Fatalf("expected exactly one success and one retryable conflict, got success=%d conflict=%d (errs=%v, %v)", okCount, conflictCount, results[0], results[1])
	}

	// The zone advanced exactly once and the revision is published once.
	liveZone, err := stack.zone.Get(fixture.zone.ID)
	if err != nil {
		t.Fatalf("reload zone: %v", err)
	}
	if liveZone.Version != fixture.zone.Version+1 {
		t.Fatalf("zone version = %d, want exactly one bump to %d", liveZone.Version, fixture.zone.Version+1)
	}
	var published, drafts, impactRows int64
	stack.db.Model(&model.ZoneRevision{}).Where("safety_zone_id = ? AND revision_status = ?", fixture.zone.ID, constants.RevisionStatusPublished).Count(&published)
	stack.db.Model(&model.ZoneRevision{}).Where("safety_zone_id = ? AND revision_status = ?", fixture.zone.ID, constants.RevisionStatusDraft).Count(&drafts)
	stack.db.Model(&model.ZoneRevisionImpact{}).Count(&impactRows)
	if published != 1 || drafts != 0 {
		t.Fatalf("expected 1 published revision and 0 open drafts, got published=%d drafts=%d", published, drafts)
	}
	if impactRows != 1 {
		t.Fatalf("expected the single publish to freeze 1 impact row, got %d", impactRows)
	}

	// The conflict is retryable: open a fresh draft and publish it successfully.
	retryDraft := gateDraftRequest()
	retryDraft.SpeedLimitMMS = 200
	if _, err := stack.service.SaveDraft(fixture.zone.ID, retryDraft, fixture.actor, "retry-draft"); err != nil {
		t.Fatalf("save retry draft: %v", err)
	}
	retryPublished, err := stack.service.Publish(fixture.zone.ID, dto.PublishZoneRevisionRequest{}, fixture.actor, "retry-publish")
	if err != nil {
		t.Fatalf("retry publish after conflict must succeed, got %v", err)
	}
	if retryPublished.PublishedVersion == nil || *retryPublished.PublishedVersion != fixture.zone.Version+2 {
		t.Fatalf("retry published version = %v, want %d", retryPublished.PublishedVersion, fixture.zone.Version+2)
	}
}

// TestFaultDuringImpactWriteRollsBackEverything injects a failure at the impact
// persistence stage. Zone, draft, impacts and re-evaluation flags must stay at
// their pre-publish state, including after a service restart re-reads the file.
func TestFaultDuringImpactWriteRollsBackEverything(t *testing.T) {
	stack := openFaultStack(t)
	defer stack.close(t)
	fixture := seedPersistentFixture(t, stack)

	_, err := stack.service.Publish(fixture.zone.ID, dto.PublishZoneRevisionRequest{}, fixture.actor, "faulty-publish")
	if err == nil {
		t.Fatal("expected the injected impact-stage failure to abort publish")
	}
	// The error must pinpoint the stage; the production service wraps the cause
	// but does not itself know anything about fault injection.
	var stageErr *faultStageError
	if !errors.As(err, &stageErr) || stageErr.Stage() != stagePersistImpacts {
		t.Fatalf("expected an error naming stage %q, got %v", stagePersistImpacts, err)
	}

	// State must be entirely pre-publish.
	assertPrePublishState(t, stack, fixture)

	// Restart the service against the same on-disk database with a clean,
	// non-faulting assembly and confirm the rollback survived: nothing partial
	// was persisted and no fault state carried over.
	restarted := stack.reopen(t)
	defer restarted.close(t)
	assertPrePublishState(t, restarted, fixture)

	// The untouched open draft is still usable through the restarted service:
	// publish succeeds, producing version + impacts.
	published, err := restarted.service.Publish(fixture.zone.ID, dto.PublishZoneRevisionRequest{}, fixture.actor, "post-restart-publish")
	if err != nil {
		t.Fatalf("publish after restart must succeed, got %v", err)
	}
	if published.PublishedVersion == nil || *published.PublishedVersion != fixture.zone.Version+1 {
		t.Fatalf("published version = %v, want %d", published.PublishedVersion, fixture.zone.Version+1)
	}
	if len(published.Impacts) != 1 || !published.Impacts[0].Affected {
		t.Fatalf("expected one frozen affected impact, got %+v", published.Impacts)
	}
	if len(published.ReevaluationFlags) != 1 || published.ReevaluationFlags[0].ValidationRunID != fixture.accepted.ID {
		t.Fatalf("expected the accepted validation to be flagged, got %+v", published.ReevaluationFlags)
	}

	// Historical evidence is still byte-for-byte present and readable.
	var run model.ValidationRun
	if err := restarted.db.First(&run, fixture.accepted.ID).Error; err != nil {
		t.Fatalf("read historical validation: %v", err)
	}
	if run.ValidationStatus != constants.ValidationAccepted || run.CollisionEventsJSON != fixture.accepted.CollisionEventsJSON {
		t.Fatalf("historical evidence was altered: status=%s evidence=%s", run.ValidationStatus, run.CollisionEventsJSON)
	}
}

func assertPrePublishState(t *testing.T, stack persistentRevisionStack, fixture persistentFixture) {
	t.Helper()
	liveZone, err := stack.zone.Get(fixture.zone.ID)
	if err != nil {
		t.Fatalf("reload zone: %v", err)
	}
	if liveZone.Version != fixture.zone.Version {
		t.Fatalf("zone version advanced to %d during rolled-back publish; want %d", liveZone.Version, fixture.zone.Version)
	}
	if liveZone.ZoneType != fixture.zone.ZoneType || liveZone.Name != fixture.zone.Name {
		t.Fatalf("zone definition changed during rolled-back publish")
	}
	var drafts, published, impacts, flags int64
	stack.db.Model(&model.ZoneRevision{}).Where("safety_zone_id = ? AND revision_status = ?", fixture.zone.ID, constants.RevisionStatusDraft).Count(&drafts)
	stack.db.Model(&model.ZoneRevision{}).Where("safety_zone_id = ? AND revision_status = ?", fixture.zone.ID, constants.RevisionStatusPublished).Count(&published)
	stack.db.Model(&model.ZoneRevisionImpact{}).Count(&impacts)
	stack.db.Model(&model.ValidationReevaluation{}).Count(&flags)
	if drafts != 1 || published != 0 || impacts != 0 || flags != 0 {
		t.Fatalf("after rolled-back publish drafts=%d published=%d impacts=%d flags=%d; want 1/0/0/0", drafts, published, impacts, flags)
	}
}

// isRetryablePublishConflict recognizes the 409 outcomes a losing concurrent
// publisher can observe, depending on scheduling relative to the winner's
// commit:
//   - version_conflict: it read the draft before the commit but its in-tx
//     conditional zone update matched zero rows because the version advanced;
//   - state_conflict:   its in-tx conditional draft publish matched zero rows
//     because the draft was already published;
//   - draft_missing:    its pre-transaction draft lookup ran after the winner
//     had already consumed the shared draft.
//
// All three are 409 conflicts: the request did not corrupt anything and the
// caller can reload and retry with a fresh draft.
func isRetryablePublishConflict(err error) bool {
	var appError *AppError
	if !errors.As(err, &appError) || appError.Status != 409 {
		return false
	}
	switch appError.Code {
	case "version_conflict", "state_conflict", "draft_missing":
		return true
	default:
		return false
	}
}
