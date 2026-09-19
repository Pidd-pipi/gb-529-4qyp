package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"lng-boiloff-gas-balance/backend/internal/balance"
	"lng-boiloff-gas-balance/backend/internal/constants"
	"lng-boiloff-gas-balance/backend/internal/dto"
	"lng-boiloff-gas-balance/backend/internal/model"
	"lng-boiloff-gas-balance/backend/internal/repository"
	"lng-boiloff-gas-balance/backend/pkg/api"
)

type balanceReleaseFixture struct {
	service *BalanceService
	db      *gorm.DB
	tank    model.StorageTank
	actor   repository.Actor
	start   time.Time
	end     time.Time
}

func newBalanceReleaseFixture(t *testing.T, name string, openingQuality, closingQuality constants.QualityFlag) balanceReleaseFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+name+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.StorageTank{}, &model.MeasurementSnapshot{}, &model.TransferOperation{}, &model.BalanceRun{}, &model.AuditEvent{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	curve, err := balance.NewCapacityCurve([]float64{0, 15000})
	if err != nil {
		t.Fatalf("capacity curve: %v", err)
	}
	raw, err := curve.Marshal()
	if err != nil {
		t.Fatalf("marshal curve: %v", err)
	}
	tank := model.StorageTank{
		TankCode: "TK-REL", Name: "放行测试罐", NominalCapacityM3: 180000, MinLevelM: 0, MaxLevelM: 12,
		ReferenceDensityKGM3: 452, ReferenceTemperatureC: -160, ThermalExpansionPerC: 0.0035,
		CapacityCurveJSON: datatypes.JSON(raw), CoefficientVersion: "CV-REL", TankStatus: "active", Version: 1,
	}
	if err := db.Create(&tank).Error; err != nil {
		t.Fatalf("create tank: %v", err)
	}
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	opening := model.MeasurementSnapshot{
		TankID: tank.ID, MeasuredAt: start.Add(-time.Hour), CalculatedLiquidMassKG: 55_000_000,
		MeasurementUncertaintyPct: 0.30, QualityFlag: openingQuality, SourceNote: "期初边界", CreatedBy: 1,
	}
	closing := model.MeasurementSnapshot{
		TankID: tank.ID, MeasuredAt: end.Add(-time.Hour), CalculatedLiquidMassKG: 55_100_000,
		MeasurementUncertaintyPct: 0.32, QualityFlag: closingQuality, SourceNote: "期末边界", CreatedBy: 1,
	}
	if err := db.Create(&opening).Error; err != nil {
		t.Fatalf("create opening snapshot: %v", err)
	}
	if err := db.Create(&closing).Error; err != nil {
		t.Fatalf("create closing snapshot: %v", err)
	}
	service := NewBalanceService(
		repository.NewBalanceRepository(db), repository.NewTankRepository(db),
		repository.NewMeasurementRepository(db), repository.NewTransferRepository(db),
	)
	actor := repository.Actor{UserID: 1, Email: "analyst@example.test", Role: constants.RoleProcessAnalyst, RequestID: "test-request"}
	return balanceReleaseFixture{service: service, db: db, tank: tank, actor: actor, start: start, end: end}
}

func (f balanceReleaseFixture) runRequest() dto.RunBalanceRequest {
	return dto.RunBalanceRequest{TankID: f.tank.ID, PeriodStart: &f.start, PeriodEnd: &f.end}
}

func (f balanceReleaseFixture) countRuns(t *testing.T) int64 {
	t.Helper()
	var count int64
	if err := f.db.Model(&model.BalanceRun{}).Count(&count).Error; err != nil {
		t.Fatalf("count balance runs: %v", err)
	}
	return count
}

func (f balanceReleaseFixture) countAudits(t *testing.T) int64 {
	t.Helper()
	var count int64
	if err := f.db.Model(&model.AuditEvent{}).Count(&count).Error; err != nil {
		t.Fatalf("count audit events: %v", err)
	}
	return count
}

func requireAPIError(t *testing.T, err error, status int, code string) *api.Error {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s error, got nil", code)
	}
	var appErr *api.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected api.Error %s, got %v", code, err)
	}
	if appErr.HTTPStatus != status || appErr.Code != code {
		t.Fatalf("error = %d/%s, want %d/%s", appErr.HTTPStatus, appErr.Code, status, code)
	}
	return appErr
}

func TestRunRejectsSuspectBoundaryWithoutReleaseNote(t *testing.T) {
	fixture := newBalanceReleaseFixture(t, "release-missing-529", constants.QualitySuspect, constants.QualityGood)
	_, err := fixture.service.Run(context.Background(), fixture.runRequest(), fixture.actor)
	appErr := requireAPIError(t, err, 422, "BOUNDARY_RELEASE_REQUIRED")
	if appErr.Details["boundary"] != "opening" {
		t.Fatalf("error details boundary = %v, want opening", appErr.Details["boundary"])
	}
	if got := fixture.countRuns(t); got != 0 {
		t.Fatalf("rejected run left %d balance run records behind", got)
	}
	if got := fixture.countAudits(t); got != 0 {
		t.Fatalf("rejected run left %d audit records behind", got)
	}
}

func TestRunRejectsSuspectClosingWithoutReleaseNote(t *testing.T) {
	fixture := newBalanceReleaseFixture(t, "release-missing-closing-529", constants.QualityGood, constants.QualitySuspect)
	request := fixture.runRequest()
	request.OpeningReleaseNote = "期初为 good，不需要依据"
	_, err := fixture.service.Run(context.Background(), request, fixture.actor)
	appErr := requireAPIError(t, err, 422, "BOUNDARY_RELEASE_REQUIRED")
	if appErr.Details["boundary"] != "closing" {
		t.Fatalf("error details boundary = %v, want closing", appErr.Details["boundary"])
	}
	if got := fixture.countRuns(t); got != 0 {
		t.Fatalf("rejected run left %d balance run records behind", got)
	}
}

func TestRunRejectsTooShortReleaseNote(t *testing.T) {
	fixture := newBalanceReleaseFixture(t, "release-short-529", constants.QualitySuspect, constants.QualitySuspect)
	request := fixture.runRequest()
	request.OpeningReleaseNote = "太短"
	request.ClosingReleaseNote = "期末温度波动已复核确认可用"
	_, err := fixture.service.Run(context.Background(), request, fixture.actor)
	requireAPIError(t, err, 422, "BOUNDARY_RELEASE_REQUIRED")
	if got := fixture.countRuns(t); got != 0 {
		t.Fatalf("rejected run left %d balance run records behind", got)
	}
}

func TestRunFreezesBoundarySnapshotsAndReleaseNotes(t *testing.T) {
	fixture := newBalanceReleaseFixture(t, "release-frozen-529", constants.QualitySuspect, constants.QualitySuspect)
	request := fixture.runRequest()
	request.OpeningReleaseNote = "期初液位计漂移已用人工检尺复核"
	request.ClosingReleaseNote = "期末温度探头波动，取稳定段均值"
	run, err := fixture.service.Run(context.Background(), request, fixture.actor)
	if err != nil {
		t.Fatalf("run with release notes: %v", err)
	}
	if run.OpeningSnapshotID == 0 || run.ClosingSnapshotID == 0 || run.OpeningSnapshotID == run.ClosingSnapshotID {
		t.Fatalf("boundary snapshot ids not frozen: opening=%d closing=%d", run.OpeningSnapshotID, run.ClosingSnapshotID)
	}
	if run.OpeningQualityFlag != constants.QualitySuspect || run.ClosingQualityFlag != constants.QualitySuspect {
		t.Fatalf("boundary quality flags not frozen: %+v", run)
	}
	if run.OpeningReleaseNote != request.OpeningReleaseNote || run.ClosingReleaseNote != request.ClosingReleaseNote {
		t.Fatalf("release notes not frozen on run: %+v", run)
	}
	var evidence balanceEvidence
	if err := json.Unmarshal(run.EvidenceJSON, &evidence); err != nil {
		t.Fatalf("decode evidence: %v", err)
	}
	openingEvidence := evidence.BoundaryRelease["opening"]
	if !openingEvidence.Released || openingEvidence.ReleaseNote != request.OpeningReleaseNote || openingEvidence.QualityFlag != constants.QualitySuspect {
		t.Fatalf("opening release evidence mismatch: %+v", openingEvidence)
	}
	closingEvidence := evidence.BoundaryRelease["closing"]
	if !closingEvidence.Released || closingEvidence.ReleaseNote != request.ClosingReleaseNote {
		t.Fatalf("closing release evidence mismatch: %+v", closingEvidence)
	}
	reloaded, err := fixture.service.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("reload run: %v", err)
	}
	if reloaded.OpeningReleaseNote != run.OpeningReleaseNote || reloaded.ClosingSnapshotID != run.ClosingSnapshotID {
		t.Fatalf("reloaded run lost frozen release data: %+v", reloaded)
	}
}

func TestRunWithGoodBoundariesKeepsOriginalFlow(t *testing.T) {
	fixture := newBalanceReleaseFixture(t, "release-good-529", constants.QualityGood, constants.QualityGood)
	request := fixture.runRequest()
	request.OpeningReleaseNote = "good 快照不需要依据，应被忽略"
	run, err := fixture.service.Run(context.Background(), request, fixture.actor)
	if err != nil {
		t.Fatalf("run with good boundaries: %v", err)
	}
	if run.OpeningReleaseNote != "" || run.ClosingReleaseNote != "" {
		t.Fatalf("good boundaries must not persist release notes: %+v", run)
	}
	if run.OpeningQualityFlag != constants.QualityGood || run.ClosingQualityFlag != constants.QualityGood {
		t.Fatalf("quality flags not frozen for good boundaries: %+v", run)
	}
	var evidence balanceEvidence
	if err := json.Unmarshal(run.EvidenceJSON, &evidence); err != nil {
		t.Fatalf("decode evidence: %v", err)
	}
	if evidence.BoundaryRelease["opening"].Released || evidence.BoundaryRelease["closing"].Released {
		t.Fatalf("good boundaries must not be marked released: %+v", evidence.BoundaryRelease)
	}
}

func TestNewSnapshotsDoNotRewriteExistingRun(t *testing.T) {
	fixture := newBalanceReleaseFixture(t, "release-recompute-529", constants.QualityGood, constants.QualityGood)
	first, err := fixture.service.Run(context.Background(), fixture.runRequest(), fixture.actor)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	newerClosing := model.MeasurementSnapshot{
		TankID: fixture.tank.ID, MeasuredAt: fixture.end.Add(-30 * time.Minute), CalculatedLiquidMassKG: 55_200_000,
		MeasurementUncertaintyPct: 0.31, QualityFlag: constants.QualityGood, SourceNote: "更接近期末的新快照", CreatedBy: 1,
	}
	if err := fixture.db.Create(&newerClosing).Error; err != nil {
		t.Fatalf("create newer closing snapshot: %v", err)
	}
	second, err := fixture.service.Run(context.Background(), fixture.runRequest(), fixture.actor)
	if err != nil {
		t.Fatalf("recompute run: %v", err)
	}
	if second.ID == first.ID {
		t.Fatal("recompute must create a new record instead of rewriting the old one")
	}
	if second.ClosingSnapshotID != newerClosing.ID {
		t.Fatalf("recompute closing snapshot = %d, want new boundary %d", second.ClosingSnapshotID, newerClosing.ID)
	}
	reloaded, err := fixture.service.Get(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("reload first run: %v", err)
	}
	if reloaded.ClosingSnapshotID != first.ClosingSnapshotID || reloaded.ClosingMassKG != first.ClosingMassKG {
		t.Fatalf("existing run was rewritten by newer snapshot: %+v", reloaded)
	}
}

func TestFrozenReleaseSurvivesReviewAndAcceptedRunLocks(t *testing.T) {
	fixture := newBalanceReleaseFixture(t, "release-review-529", constants.QualitySuspect, constants.QualityGood)
	request := fixture.runRequest()
	request.OpeningReleaseNote = "期初快照经人工复测确认可用"
	run, err := fixture.service.Run(context.Background(), request, fixture.actor)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	submitted, err := fixture.service.Submit(context.Background(), run.ID, dto.SubmitBalanceRequest{Version: run.Version}, fixture.actor)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if submitted.OpeningReleaseNote != request.OpeningReleaseNote || submitted.OpeningSnapshotID != run.OpeningSnapshotID {
		t.Fatalf("submit rewrote frozen release data: %+v", submitted)
	}
	reviewer := repository.Actor{UserID: 2, Email: "reviewer@example.test", Role: constants.RoleReviewer, RequestID: "review-request"}
	accepted, err := fixture.service.Review(context.Background(), run.ID, dto.ReviewBalanceRequest{
		TargetStatus: constants.BalanceAccepted, Version: submitted.Version, ReviewNote: "放行依据与偏差关系已核对",
	}, reviewer)
	if err != nil {
		t.Fatalf("review accept: %v", err)
	}
	if accepted.OpeningReleaseNote != request.OpeningReleaseNote || accepted.OpeningQualityFlag != constants.QualitySuspect {
		t.Fatalf("review rewrote frozen release data: %+v", accepted)
	}
	if _, err := fixture.service.Submit(context.Background(), run.ID, dto.SubmitBalanceRequest{Version: accepted.Version}, fixture.actor); err == nil {
		t.Fatal("accepted run must not accept further transitions")
	}
	admin := repository.Actor{UserID: 3, Email: "admin@example.test", Role: constants.RoleAdmin, RequestID: "admin-request"}
	if _, err := fixture.service.Invalidate(context.Background(), run.ID, dto.InvalidateBalanceRequest{Version: accepted.Version, Reason: "尝试作废已接受结果"}, admin); err == nil {
		t.Fatal("accepted run must remain immutable")
	}
	reloaded, err := fixture.service.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("reload accepted run: %v", err)
	}
	if reloaded.BalanceStatus != constants.BalanceAccepted || reloaded.OpeningReleaseNote != request.OpeningReleaseNote {
		t.Fatalf("accepted run changed after locked transitions: %+v", reloaded)
	}
}

func TestBoundaryPreviewFlagsSuspectSnapshots(t *testing.T) {
	fixture := newBalanceReleaseFixture(t, "release-preview-529", constants.QualitySuspect, constants.QualityGood)
	preview, err := fixture.service.BoundaryPreview(context.Background(), fixture.tank.ID, fixture.start, fixture.end)
	if err != nil {
		t.Fatalf("boundary preview: %v", err)
	}
	if !preview.OpeningReleaseRequired || preview.ClosingReleaseRequired {
		t.Fatalf("preview release flags mismatch: %+v", preview)
	}
	if preview.Opening.QualityFlag != constants.QualitySuspect || preview.Closing.QualityFlag != constants.QualityGood {
		t.Fatalf("preview snapshots mismatch: %+v", preview)
	}
	if _, err := fixture.service.BoundaryPreview(context.Background(), fixture.tank.ID, fixture.end, fixture.start); err == nil {
		t.Fatal("preview must reject inverted periods")
	}
}
