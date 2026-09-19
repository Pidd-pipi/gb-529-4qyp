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

func releaseTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:balance-release-" + t.Name() + "?mode=memory&cache=shared&_foreign_keys=on"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.StorageTank{}, &model.MeasurementSnapshot{},
		&model.TransferOperation{}, &model.BalanceRun{}, &model.AuditEvent{},
	); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	return db
}

func releaseFixture(t *testing.T, db *gorm.DB, closingQuality constants.QualityFlag) (model.StorageTank, model.MeasurementSnapshot, model.MeasurementSnapshot, time.Time, time.Time) {
	t.Helper()
	curve, _ := balance.NewCapacityCurve([]float64{0, 15000})
	raw, _ := curve.Marshal()
	tank := model.StorageTank{
		TankCode: "TK-REL", Name: "释放规则储罐", NominalCapacityM3: 180000, MinLevelM: 0, MaxLevelM: 12,
		ReferenceDensityKGM3: 452, ReferenceTemperatureC: -160, ThermalExpansionPerC: 0.0035,
		CapacityCurveJSON: datatypes.JSON(raw), CoefficientVersion: "CV-REL-1", TankStatus: "active", Version: 1,
	}
	if err := db.Create(&tank).Error; err != nil {
		t.Fatalf("create tank: %v", err)
	}
	start := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	opening := model.MeasurementSnapshot{
		TankID: tank.ID, MeasuredAt: start.Add(-time.Hour), LiquidLevelM: 8.2, LiquidTempC: -160.2,
		VaporPressureKPA: 111, DensityKGM3: 451.8, CalculatedVolumeM3: 123000, TemperatureDensityKGM3: 451.0,
		CalculatedLiquidMassKG: 55_000_000, MeasurementUncertaintyPct: 0.30, QualityFlag: constants.QualityGood,
		SourceNote: "期初良好快照", CreatedBy: 1,
	}
	closing := model.MeasurementSnapshot{
		TankID: tank.ID, MeasuredAt: end.Add(-time.Hour), LiquidLevelM: 8.1, LiquidTempC: -159.8,
		VaporPressureKPA: 113, DensityKGM3: 451.2, CalculatedVolumeM3: 121500, TemperatureDensityKGM3: 450.4,
		CalculatedLiquidMassKG: 54_900_000, MeasurementUncertaintyPct: 0.32, QualityFlag: closingQuality,
		SourceNote: "期末可疑快照", CreatedBy: 1,
	}
	if err := db.Create(&opening).Error; err != nil {
		t.Fatalf("create opening snapshot: %v", err)
	}
	if err := db.Create(&closing).Error; err != nil {
		t.Fatalf("create closing snapshot: %v", err)
	}
	return tank, opening, closing, start, end
}

func newReleaseService(db *gorm.DB) *BalanceService {
	return NewBalanceService(
		repository.NewBalanceRepository(db),
		repository.NewTankRepository(db),
		repository.NewMeasurementRepository(db),
		repository.NewTransferRepository(db),
	)
}

func releaseActor() repository.Actor {
	return repository.Actor{UserID: 1, Email: "analyst@lng.local", Role: constants.RoleProcessAnalyst, RequestID: "test-release"}
}

func assertErrorCode(t *testing.T, err error, code string) {
	t.Helper()
	var appErr *api.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected api error %s, got %v", code, err)
	}
	if appErr.Code != code {
		t.Fatalf("error code = %s, want %s", appErr.Code, code)
	}
}

func TestRunRejectsSuspectBoundaryWithoutBasisAndLeavesNoRecord(t *testing.T) {
	db := releaseTestDB(t)
	_, _, _, start, end := releaseFixture(t, db, constants.QualitySuspect)
	service := newReleaseService(db)
	request := dto.RunBalanceRequest{TankID: 1, PeriodStart: &start, PeriodEnd: &end}

	if _, err := service.Run(context.Background(), request, releaseActor()); err == nil {
		t.Fatal("suspect closing snapshot without release basis must be rejected")
	} else {
		assertErrorCode(t, err, "CLOSING_RELEASE_BASIS_REQUIRED")
	}

	var count int64
	if err := db.Model(&model.BalanceRun{}).Count(&count).Error; err != nil {
		t.Fatalf("count runs: %v", err)
	}
	if count != 0 {
		t.Fatalf("rejected run must not leave a partial balance record, found %d", count)
	}
	var auditCount int64
	if err := db.Model(&model.AuditEvent{}).Where("entity_type = ?", "balance_run").Count(&auditCount).Error; err != nil {
		t.Fatalf("count audits: %v", err)
	}
	if auditCount != 0 {
		t.Fatalf("rejected run must not leave audit evidence, found %d", auditCount)
	}
}

func TestRunAcceptsGoodBoundaryWithoutBasisAndFreezesRelease(t *testing.T) {
	db := releaseTestDB(t)
	tank, _, _, start, end := releaseFixture(t, db, constants.QualityGood)
	service := newReleaseService(db)
	request := dto.RunBalanceRequest{TankID: tank.ID, PeriodStart: &start, PeriodEnd: &end}

	run, err := service.Run(context.Background(), request, releaseActor())
	if err != nil {
		t.Fatalf("good boundary run: %v", err)
	}
	if run.OpeningReleaseBasis != "" || run.ClosingReleaseBasis != "" {
		t.Fatal("good snapshots must follow the original flow without release basis")
	}
	var evidence balanceEvidence
	if err := json.Unmarshal(run.EvidenceJSON, &evidence); err != nil {
		t.Fatalf("decode evidence: %v", err)
	}
	if evidence.BoundaryReleases.Closing.Released || evidence.BoundaryReleases.Opening.Released {
		t.Fatal("good boundary snapshots must not be marked as released")
	}
	if evidence.BoundaryReleases.Closing.SnapshotID != run.ClosingSnapshotID {
		t.Fatal("evidence must freeze the closing snapshot identity")
	}
}

func TestRunFreezesSuspectBoundaryReleaseBasis(t *testing.T) {
	db := releaseTestDB(t)
	tank, _, closing, start, end := releaseFixture(t, db, constants.QualitySuspect)
	service := newReleaseService(db)
	basis := "液位计 08:00 校准记录复核，温度漂移在工艺允许范围内，值班长签字放行。"
	request := dto.RunBalanceRequest{
		TankID: tank.ID, PeriodStart: &start, PeriodEnd: &end, ClosingReleaseBasis: basis,
	}
	run, err := service.Run(context.Background(), request, releaseActor())
	if err != nil {
		t.Fatalf("suspect boundary with basis: %v", err)
	}
	if run.ClosingSnapshotID != closing.ID || run.ClosingReleaseBasis != basis {
		t.Fatalf("run did not freeze suspect snapshot release: id=%d basis=%q", run.ClosingSnapshotID, run.ClosingReleaseBasis)
	}
	var evidence balanceEvidence
	if err := json.Unmarshal(run.EvidenceJSON, &evidence); err != nil {
		t.Fatalf("decode evidence: %v", err)
	}
	release := evidence.BoundaryReleases.Closing
	if !release.Released || release.QualityFlag != constants.QualitySuspect || release.ReleaseBasis != basis || release.SnapshotID != closing.ID {
		t.Fatalf("evidence boundary release is incomplete: %+v", release)
	}
}

func TestNewSnapshotNeverRewritesPriorRun(t *testing.T) {
	db := releaseTestDB(t)
	tank, _, firstClosing, start, end := releaseFixture(t, db, constants.QualityGood)
	service := newReleaseService(db)
	firstRun, err := service.Run(context.Background(),
		dto.RunBalanceRequest{TankID: tank.ID, PeriodStart: &start, PeriodEnd: &end}, releaseActor())
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	firstClosingMass := firstRun.ClosingMassKG

	// 运行建立后新增一条更晚的期末快照；旧运行必须保持原结果，重算另建记录。
	newClosing := model.MeasurementSnapshot{
		TankID: tank.ID, MeasuredAt: end.Add(-30 * time.Minute), LiquidLevelM: 8.05, LiquidTempC: -159.7,
		VaporPressureKPA: 113, DensityKGM3: 451.0, CalculatedVolumeM3: 120800, TemperatureDensityKGM3: 450.2,
		CalculatedLiquidMassKG: 54_600_000, MeasurementUncertaintyPct: 0.31, QualityFlag: constants.QualityGood,
		SourceNote: "新增期末快照", CreatedBy: 1,
	}
	if err := db.Create(&newClosing).Error; err != nil {
		t.Fatalf("create later closing snapshot: %v", err)
	}
	secondRun, err := service.Run(context.Background(),
		dto.RunBalanceRequest{TankID: tank.ID, PeriodStart: &start, PeriodEnd: &end}, releaseActor())
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if secondRun.ID == firstRun.ID {
		t.Fatal("recalculation must create a separate balance record")
	}
	if secondRun.ClosingSnapshotID != newClosing.ID {
		t.Fatalf("second run must bind the new snapshot, got %d", secondRun.ClosingSnapshotID)
	}
	reloaded, err := service.Get(context.Background(), firstRun.ID)
	if err != nil {
		t.Fatalf("reload first run: %v", err)
	}
	if reloaded.ClosingSnapshotID != firstClosing.ID || reloaded.ClosingMassKG != firstClosingMass || reloaded.Version != 2 {
		t.Fatal("new snapshot rewrote the frozen boundary result of the prior run")
	}
}

func TestPendingReviewAndAcceptedRunsAreImmutable(t *testing.T) {
	if constants.CanTransitionBalance(constants.BalancePendingReview, constants.BalanceCalculating) {
		t.Fatal("pending_review run must not move back to calculating")
	}
	if constants.CanTransitionBalance(constants.BalanceAccepted, constants.BalanceRejected) {
		t.Fatal("accepted run must not be changed to rejected")
	}
	if constants.CanTransitionBalance(constants.BalanceAccepted, constants.BalancePendingReview) {
		t.Fatal("accepted run must not re-enter review")
	}
}

func TestResolveBoundaryReleasesRules(t *testing.T) {
	good := model.MeasurementSnapshot{ID: 1, QualityFlag: constants.QualityGood}
	suspect := model.MeasurementSnapshot{ID: 2, QualityFlag: constants.QualitySuspect}

	if _, _, err := resolveBoundaryReleases(dto.RunBalanceRequest{}, good, suspect); err == nil {
		t.Fatal("suspect closing without basis must fail")
	} else {
		assertErrorCode(t, err, "CLOSING_RELEASE_BASIS_REQUIRED")
	}
	if _, _, err := resolveBoundaryReleases(dto.RunBalanceRequest{OpeningReleaseBasis: "  "}, suspect, good); err == nil {
		t.Fatal("suspect opening without basis must fail")
	} else {
		assertErrorCode(t, err, "OPENING_RELEASE_BASIS_REQUIRED")
	}
	// good 边界即使带了多余依据也沿用原流程，依据被忽略；拒绝只针对本次运行。
	openingBasis, closingBasis, err := resolveBoundaryReleases(
		dto.RunBalanceRequest{OpeningReleaseBasis: "多余依据", ClosingReleaseBasis: " "}, good, good)
	if err != nil || openingBasis != "" || closingBasis != "" {
		t.Fatalf("good boundary basis must be ignored, got %q %q err=%v", openingBasis, closingBasis, err)
	}
	openingBasis, closingBasis, err = resolveBoundaryReleases(
		dto.RunBalanceRequest{OpeningReleaseBasis: "期初校准复核", ClosingReleaseBasis: "期末值班长签字"}, suspect, suspect)
	if err != nil || openingBasis != "期初校准复核" || closingBasis != "期末值班长签字" {
		t.Fatalf("suspect basis must be preserved, got %q %q err=%v", openingBasis, closingBasis, err)
	}
}
