package service

import (
	"aquaculture-water-feeding-control/backend/internal/constants"
	"aquaculture-water-feeding-control/backend/internal/dto"
	"aquaculture-water-feeding-control/backend/internal/model"
	"aquaculture-water-feeding-control/backend/internal/repository"
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// 真实 PostgreSQL 集成测试，通过 AQUA_TEST_DATABASE_URL 启用，例如：
// AQUA_TEST_DATABASE_URL="host=/tmp/pgrun port=15432 user=aqua password=aqua dbname=aqua sslmode=disable"
//
// 每个测试创建独立的一次性数据库，保证 go test ./... 并行执行多个测试包时互不干扰。
func integrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	baseDSN := os.Getenv("AQUA_TEST_DATABASE_URL")
	if baseDSN == "" {
		t.Skip("set AQUA_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	sandboxName := openSandboxDatabase(t, baseDSN)
	dsn := withDatabaseName(baseDSN, sandboxName)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sandbox database: %v", err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.Pond{}, &model.WaterReading{}, &model.FeedingPlan{},
		&model.ControlExecution{}, &model.FeedingRestriction{}, &model.AuditLog{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_feeding_restriction_one_active
		ON feeding_restrictions (pond_id)
		WHERE status IN ('active', 'disposed')
	`).Error; err != nil {
		t.Fatalf("index: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		admin, err := gorm.Open(postgres.Open(baseDSN), &gorm.Config{})
		if err != nil {
			return
		}
		_ = admin.Exec(fmt.Sprintf(`DROP DATABASE %s WITH (FORCE)`, sandboxName)).Error
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

var dsnDBNamePattern = regexp.MustCompile(`dbname=\S+`)

func withDatabaseName(dsn, name string) string {
	if dsnDBNamePattern.MatchString(dsn) {
		return dsnDBNamePattern.ReplaceAllString(dsn, "dbname="+name)
	}
	return strings.TrimSpace(dsn) + " dbname=" + name
}

// openSandboxDatabase 通过管理连接创建一个名称唯一的空数据库。
func openSandboxDatabase(t *testing.T, baseDSN string) string {
	t.Helper()
	admin, err := gorm.Open(postgres.Open(baseDSN), &gorm.Config{})
	if err != nil {
		t.Fatalf("open maintenance database: %v", err)
	}
	name := fmt.Sprintf("aqua_test_%d_%d", time.Now().UnixNano(), rand.Intn(1_000_000))
	if err := admin.Exec(fmt.Sprintf(`CREATE DATABASE %s`, name)).Error; err != nil {
		t.Fatalf("create sandbox database: %v", err)
	}
	if sqlDB, err := admin.DB(); err == nil {
		_ = sqlDB.Close()
	}
	return name
}

type restrictionStack struct {
	db           *gorm.DB
	ponds        *repository.PondRepository
	readings     *repository.ReadingRepository
	plans        *repository.PlanRepository
	executions   *repository.ExecutionRepository
	restrictions *repository.RestrictionRepository
	audit        *AuditService
	readingSvc   *ReadingService
	planSvc      *PlanService
	execSvc      *ExecutionService
	restSvc      *RestrictionService
}

func newRestrictionStack(t *testing.T) *restrictionStack {
	db := integrationDB(t)
	pondRepo := repository.NewPondRepository(db)
	readingRepo := repository.NewReadingRepository(db)
	planRepo := repository.NewPlanRepository(db)
	execRepo := repository.NewExecutionRepository(db)
	restRepo := repository.NewRestrictionRepository(db)
	auditSvc := NewAuditService(repository.NewAuditRepository(db))
	restSvc := NewRestrictionService(restRepo, pondRepo, readingRepo, auditSvc)
	return &restrictionStack{
		db: db, ponds: pondRepo, readings: readingRepo, plans: planRepo, executions: execRepo,
		restrictions: restRepo, audit: auditSvc, restSvc: restSvc,
		readingSvc: NewReadingService(readingRepo, pondRepo, restSvc, auditSvc),
		planSvc:    NewPlanService(planRepo, pondRepo, readingRepo, restRepo, auditSvc),
		execSvc:    NewExecutionService(execRepo, planRepo, pondRepo, readingRepo, restRepo, auditSvc),
	}
}

func testActor(name, role string, requestID string) Actor {
	return Actor{Username: name, DisplayName: name, Role: role, RequestID: requestID}
}

func createTestPond(t *testing.T, st *restrictionStack, suffix string) model.Pond {
	t.Helper()
	pond := model.Pond{
		Code: fmt.Sprintf("P-IT-%s-%d", suffix, time.Now().UnixNano()), Name: "集成测试塘" + suffix,
		Species: "测试鱼", AreaSquareMeters: 1000, CapacityKg: 10000, GrowthStage: "成长期",
		Status: constants.PondStatusActive, Manager: "塘主" + suffix,
	}
	if err := st.ponds.Create(&pond); err != nil {
		t.Fatalf("create pond: %v", err)
	}
	return pond
}

func readingInput(pondID uint, oxygen, ammonia float64, at time.Time) dto.WaterReadingInput {
	return dto.WaterReadingInput{
		PondID: pondID, DissolvedOxygen: oxygen, Temperature: 26, PH: 7.4, Ammonia: ammonia,
		Turbidity: 20, MeasuredAt: at, Source: "manual",
	}
}

func countRestrictions(t *testing.T, st *restrictionStack, pondID uint) (total, open int64) {
	t.Helper()
	if err := st.db.Model(&model.FeedingRestriction{}).Where("pond_id = ?", pondID).Count(&total).Error; err != nil {
		t.Fatalf("count restrictions: %v", err)
	}
	if err := st.db.Model(&model.FeedingRestriction{}).Where("pond_id = ? AND status IN ?", pondID, []string{"active", "disposed"}).Count(&open).Error; err != nil {
		t.Fatalf("count open restrictions: %v", err)
	}
	return
}

// 严重读数自动建闸，重复严重读数不产生重复记录。
func TestIntegrationCriticalReadingEstablishesSingleRestriction(t *testing.T) {
	st := newRestrictionStack(t)
	operator := testActor("值班操作员", "operator", "req-critical-1")
	pond := createTestPond(t, st, "A")
	now := time.Now().UTC()

	if _, err := st.readingSvc.Create(readingInput(pond.ID, 2.4, 0.1, now.Add(-2*time.Hour)), operator); err != nil {
		t.Fatalf("critical reading: %v", err)
	}
	total, open := countRestrictions(t, st, pond.ID)
	if total != 1 || open != 1 {
		t.Fatalf("after first critical: total=%d open=%d, want 1/1", total, open)
	}

	// 同池重复严重读数：必须幂等，不新增限制、不重复审计。
	if _, err := st.readingSvc.Create(readingInput(pond.ID, 2.1, 1.5, now.Add(-time.Hour)), operator); err != nil {
		t.Fatalf("second critical reading: %v", err)
	}
	total, open = countRestrictions(t, st, pond.ID)
	if total != 1 || open != 1 {
		t.Fatalf("after second critical: total=%d open=%d, want 1/1", total, open)
	}
	var restrictAudits int64
	if err := st.db.Model(&model.AuditLog{}).Where("entity_type = ? AND action = ?", "feeding_restriction", "restrict").Count(&restrictAudits).Error; err != nil {
		t.Fatalf("count audits: %v", err)
	}
	if restrictAudits != 1 {
		t.Fatalf("restrict audits = %d, want 1", restrictAudits)
	}
}

// 并发录入多条严重读数：只有一个成功建立限制，且失败不得阻断读数、不得产生重复限制。
func TestIntegrationConcurrentCriticalReadingsCreateOneRestriction(t *testing.T) {
	st := newRestrictionStack(t)
	pond := createTestPond(t, st, "B")
	now := time.Now().UTC()

	const n = 6
	var wg sync.WaitGroup
	errs := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			actor := testActor(fmt.Sprintf("操作员%d", i), "operator", fmt.Sprintf("req-conc-%d", i))
			_, errs[i] = st.readingSvc.Create(
				readingInput(pond.ID, 1.8+float64(i)*0.1, 1.6, now.Add(-time.Duration(n-i)*time.Minute)),
				actor,
			)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("reading %d failed (readings must never be blocked): %v", i, err)
		}
	}
	var readings int64
	if err := st.db.Model(&model.WaterReading{}).Where("pond_id = ?", pond.ID).Count(&readings).Error; err != nil {
		t.Fatalf("count readings: %v", err)
	}
	if readings != n {
		t.Fatalf("readings = %d, want %d", readings, n)
	}
	total, open := countRestrictions(t, st, pond.ID)
	if total != 1 || open != 1 {
		t.Fatalf("concurrent critical: total=%d open=%d, want 1/1", total, open)
	}
}

// 生效中的闸门同时阻断计划批准和新建投喂执行；计划批准与执行安排各只有一次成功机会。
func TestIntegrationRestrictionBlocksApproveAndExecution(t *testing.T) {
	st := newRestrictionStack(t)
	pond := createTestPond(t, st, "C")
	now := time.Now().UTC()
	operator := testActor("值班操作员", "operator", "req-gate-1")
	manager := testActor("生产主管", "manager", "req-gate-2")

	// 异常前先完成一条已批准计划。
	if _, err := st.readingSvc.Create(readingInput(pond.ID, 7.0, 0.1, now.Add(-30*time.Minute)), operator); err != nil {
		t.Fatalf("normal reading: %v", err)
	}
	plan, err := st.planSvc.Create(dto.FeedingPlanInput{
		PondID: pond.ID, Name: "闸门测试计划", DailyAmountKg: 40, FrequencyPerDay: 2, FeedType: "配合饲料",
		TargetGrowthStage: "成长期", MinOxygen: 5, StartDate: now.Add(-time.Hour), EndDate: now.Add(72 * time.Hour),
		Rationale: "正常水质下制定的投喂策略，依据充分",
	}, operator)
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if _, err := st.planSvc.Submit(plan.ID, "提交审核", operator); err != nil {
		t.Fatalf("submit plan: %v", err)
	}
	if _, err := st.planSvc.Approve(plan.ID, "水质正常，同意", manager); err != nil {
		t.Fatalf("approve before restriction: %v", err)
	}

	// 严重异常建闸。
	if _, err := st.readingSvc.Create(readingInput(pond.ID, 2.2, 0.1, now.Add(-20*time.Minute)), operator); err != nil {
		t.Fatalf("critical reading: %v", err)
	}

	// 已批准计划不能再安排新执行。
	_, err = st.execSvc.Create(dto.ExecutionInput{
		PondID: pond.ID, FeedingPlanID: plan.ID, ScheduledAt: now.Add(2 * time.Hour),
		PlannedAmountKg: 10, Weather: "晴朗",
	}, operator)
	if err == nil {
		t.Fatal("execution create must be blocked by active restriction")
	}
	assertConflict(t, err, "停喂限制")

	// 新计划提交后，批准同样被闸门拦截（即使最新读数严重也由闸门优先阻断）。
	plan2, err := st.planSvc.Create(dto.FeedingPlanInput{
		PondID: pond.ID, Name: "闸门期新计划", DailyAmountKg: 30, FrequencyPerDay: 2, FeedType: "配合饲料",
		TargetGrowthStage: "成长期", MinOxygen: 5, StartDate: now.Add(-time.Hour), EndDate: now.Add(72 * time.Hour),
		Rationale: "停喂期间不应被批准的计划，依据充分",
	}, operator)
	if err != nil {
		t.Fatalf("create plan2: %v", err)
	}
	if _, err := st.planSvc.Submit(plan2.ID, "提交", operator); err != nil {
		t.Fatalf("submit plan2: %v", err)
	}
	_, err = st.planSvc.Approve(plan2.ID, "尝试批准", manager)
	if err == nil {
		t.Fatal("approve must be blocked by active restriction")
	}
	assertConflict(t, err, "停喂限制")
}

// 完整生命周期：active →（操作员处置）→ disposed →（后续正常读数+主管复核依据）→ released。
func TestIntegrationDisposeThenReleaseLifecycle(t *testing.T) {
	st := newRestrictionStack(t)
	pond := createTestPond(t, st, "D")
	now := time.Now().UTC()
	operator := testActor("值班操作员", "operator", "req-life-1")
	manager := testActor("生产主管", "manager", "req-life-2")

	if _, err := st.readingSvc.Create(readingInput(pond.ID, 2.0, 1.5, now.Add(-3*time.Hour)), operator); err != nil {
		t.Fatalf("critical reading: %v", err)
	}
	restriction, active, err := st.restSvc.ActiveForPond(pond.ID)
	if err != nil || !active {
		t.Fatalf("active restriction missing: %v", err)
	}

	// 未处置直接解除必须失败。
	if _, err := st.restSvc.Release(restriction.ID, "尚未处置就想解除", manager); err == nil {
		t.Fatal("release before disposition must fail")
	}

	// 处置后最新仍是严重读数，解除失败。
	if _, err := st.restSvc.Dispose(restriction.ID, "已开启增氧机并换水 30%，持续观察", operator); err != nil {
		t.Fatalf("dispose: %v", err)
	}
	if _, err := st.restSvc.Release(restriction.ID, "尝试解除", manager); err == nil {
		t.Fatal("release without later normal reading must fail")
	}

	// 后续预警读数也不允许解除。
	if _, err := st.readingSvc.Create(readingInput(pond.ID, 4.2, 0.1, now.Add(-time.Hour)), operator); err != nil {
		t.Fatalf("warning reading: %v", err)
	}
	if _, err := st.restSvc.Release(restriction.ID, "预警仍在尝试解除", manager); err == nil {
		t.Fatal("release with warning latest reading must fail")
	}

	// 触发异常之前的旧正常读数不能冒充后续读数：构造一条更早的正常读数再用更晚的异常覆盖最新。
	// 这里直接验证：录入晚于触发时间的正常读数后解除成功。
	if _, err := st.readingSvc.Create(readingInput(pond.ID, 7.2, 0.08, now.Add(-30*time.Minute)), operator); err != nil {
		t.Fatalf("normal reading: %v", err)
	}
	released, err := st.restSvc.Release(restriction.ID, "连续两次复测溶解氧 7.0 以上、氨氮回落，现场摄食恢复", manager)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if released.Status != constants.RestrictionReleased || released.NormalReadingID == nil {
		t.Fatalf("released restriction wrong state: %+v", released)
	}
	if released.ReleasedBy != "生产主管" {
		t.Fatalf("releasedBy = %q", released.ReleasedBy)
	}
	if _, active, err := st.restSvc.ActiveForPond(pond.ID); err != nil {
		t.Fatalf("active lookup: %v", err)
	} else if active {
		t.Fatal("released restriction must no longer be active")
	}

	// 解除后闸门放行：可以批准计划并安排执行。
	plan, err := st.planSvc.Create(dto.FeedingPlanInput{
		PondID: pond.ID, Name: "恢复投喂计划", DailyAmountKg: 40, FrequencyPerDay: 2, FeedType: "配合饲料",
		TargetGrowthStage: "成长期", MinOxygen: 5, StartDate: now.Add(-time.Hour), EndDate: now.Add(72 * time.Hour),
		Rationale: "水质恢复正常后恢复投喂，依据充分",
	}, operator)
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if _, err := st.planSvc.Submit(plan.ID, "恢复提交", operator); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := st.planSvc.Approve(plan.ID, "复测正常，恢复投喂", manager); err != nil {
		t.Fatalf("approve after release: %v", err)
	}
	if _, err := st.execSvc.Create(dto.ExecutionInput{
		PondID: pond.ID, FeedingPlanID: plan.ID, ScheduledAt: now.Add(2 * time.Hour),
		PlannedAmountKg: 10, Weather: "晴朗",
	}, operator); err != nil {
		t.Fatalf("execution after release: %v", err)
	}
}

// 并发处置与并发解除各只有一次成功，其余必须冲突失败且状态一致。
func TestIntegrationConcurrentDisposeAndReleaseSingleWinner(t *testing.T) {
	st := newRestrictionStack(t)
	pond := createTestPond(t, st, "E")
	now := time.Now().UTC()
	operator := testActor("值班操作员", "operator", "req-single-1")

	if _, err := st.readingSvc.Create(readingInput(pond.ID, 1.9, 1.4, now.Add(-2*time.Hour)), operator); err != nil {
		t.Fatalf("critical reading: %v", err)
	}
	restriction, _, _ := st.restSvc.ActiveForPond(pond.ID)

	const n = 5
	var wg sync.WaitGroup
	results := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			actor := testActor("值班操作员", "operator", fmt.Sprintf("req-dispose-%d", i))
			_, results[i] = st.restSvc.Dispose(restriction.ID, fmt.Sprintf("处置措施编号 %d：增氧换水并巡检", i), actor)
		}()
	}
	wg.Wait()
	successes, conflicts := 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			successes++
		case isConflict(err):
			conflicts++
		default:
			t.Fatalf("unexpected dispose error: %v", err)
		}
	}
	if successes != 1 || conflicts != n-1 {
		t.Fatalf("dispose successes=%d conflicts=%d, want 1 and %d", successes, conflicts, n-1)
	}
	var disposed model.FeedingRestriction
	if err := st.db.First(&disposed, restriction.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if disposed.Status != constants.RestrictionDisposed || disposed.DispositionNote == "" {
		t.Fatalf("disposed state inconsistent: %+v", disposed)
	}

	// 录入后续正常读数，再并发解除。
	if _, err := st.readingSvc.Create(readingInput(pond.ID, 7.0, 0.05, now.Add(-10*time.Minute)), operator); err != nil {
		t.Fatalf("normal reading: %v", err)
	}
	wg.Add(n)
	releaseResults := make([]error, n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			actor := testActor("生产主管", "manager", fmt.Sprintf("req-release-%d", i))
			_, releaseResults[i] = st.restSvc.Release(restriction.ID, fmt.Sprintf("复核依据 %d：复测正常且现场稳定", i), actor)
		}()
	}
	wg.Wait()
	successes, conflicts = 0, 0
	for _, err := range releaseResults {
		switch {
		case err == nil:
			successes++
		case isConflict(err):
			conflicts++
		default:
			t.Fatalf("unexpected release error: %v", err)
		}
	}
	if successes != 1 || conflicts != n-1 {
		t.Fatalf("release successes=%d conflicts=%d, want 1 and %d", successes, conflicts, n-1)
	}
	if err := st.db.First(&disposed, restriction.ID).Error; err != nil {
		t.Fatalf("reload after release: %v", err)
	}
	if disposed.Status != constants.RestrictionReleased || disposed.ReleaseBasis == "" || disposed.NormalReadingID == nil {
		t.Fatalf("released state inconsistent: %+v", disposed)
	}
}

// 解除后再次发生严重异常，应能对同一池塘建立新的限制记录。
func TestIntegrationNewRestrictionAfterRelease(t *testing.T) {
	st := newRestrictionStack(t)
	pond := createTestPond(t, st, "F")
	now := time.Now().UTC()
	operator := testActor("值班操作员", "operator", "req-rearm-1")
	manager := testActor("生产主管", "manager", "req-rearm-2")

	if _, err := st.readingSvc.Create(readingInput(pond.ID, 2.0, 1.5, now.Add(-4*time.Hour)), operator); err != nil {
		t.Fatalf("critical reading: %v", err)
	}
	first, _, _ := st.restSvc.ActiveForPond(pond.ID)
	if _, err := st.restSvc.Dispose(first.ID, "增氧换水处置", operator); err != nil {
		t.Fatalf("dispose: %v", err)
	}
	if _, err := st.readingSvc.Create(readingInput(pond.ID, 7.0, 0.05, now.Add(-3*time.Hour)), operator); err != nil {
		t.Fatalf("normal reading: %v", err)
	}
	if _, err := st.restSvc.Release(first.ID, "复测恢复正常，解除", manager); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := st.readingSvc.Create(readingInput(pond.ID, 2.3, 1.2, now.Add(-time.Hour)), operator); err != nil {
		t.Fatalf("second critical reading: %v", err)
	}
	second, active, err := st.restSvc.ActiveForPond(pond.ID)
	if err != nil || !active {
		t.Fatalf("new restriction after release missing: %v", err)
	}
	if second.ID == first.ID {
		t.Fatal("expected a new restriction record after release")
	}
	total, open := countRestrictions(t, st, pond.ID)
	if total != 2 || open != 1 {
		t.Fatalf("total=%d open=%d, want 2/1", total, open)
	}
}

func assertConflict(t *testing.T, err error, contains string) {
	t.Helper()
	if !isConflict(err) {
		t.Fatalf("want conflict containing %q, got %v", contains, err)
	}
	appErr, ok := AsAppError(err)
	if !ok || !strings.Contains(appErr.Message, contains) {
		t.Fatalf("conflict message = %v, want contains %q", err, contains)
	}
}

func isConflict(err error) bool {
	appErr, ok := AsAppError(err)
	return ok && appErr.Code == CodeConflict
}
