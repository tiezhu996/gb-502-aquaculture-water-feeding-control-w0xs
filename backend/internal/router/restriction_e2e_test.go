package router_test

import (
	"aquaculture-water-feeding-control/backend/internal/constants"
	"aquaculture-water-feeding-control/backend/internal/handler"
	"aquaculture-water-feeding-control/backend/internal/middleware"
	"aquaculture-water-feeding-control/backend/internal/model"
	"aquaculture-water-feeding-control/backend/internal/repository"
	"aquaculture-water-feeding-control/backend/internal/service"
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type e2eEnv struct {
	t      *testing.T
	server *httptest.Server
	db     *gorm.DB
	pondID uint
	tokens map[string]string
}

func setupE2E(t *testing.T) *e2eEnv {
	t.Helper()
	baseDSN := os.Getenv("AQUA_TEST_DATABASE_URL")
	if baseDSN == "" {
		t.Skip("set AQUA_TEST_DATABASE_URL to run router integration tests")
	}
	sandboxName := openSandboxDatabase(t, baseDSN)
	dsn := withDatabaseName(baseDSN, sandboxName)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
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
		if err == nil {
			_ = admin.Exec(fmt.Sprintf(`DROP DATABASE %s WITH (FORCE)`, sandboxName)).Error
			if sqlDB, err := admin.DB(); err == nil {
				_ = sqlDB.Close()
			}
		}
	})

	users := []struct {
		username, display, password string
		role                        constants.Role
	}{
		{"admin", "系统管理员", "admin123", constants.RoleAdmin},
		{"manager", "生产主管", "manager123", constants.RoleManager},
		{"operator", "值班操作员", "operator123", constants.RoleOperator},
		{"viewer", "观察员", "viewer123", constants.RoleViewer},
	}
	for _, u := range users {
		hash, _ := bcrypt.GenerateFromPassword([]byte(u.password), bcrypt.DefaultCost)
		if err := db.Create(&model.User{Username: u.username, DisplayName: u.display, PasswordHash: string(hash), Role: u.role, Active: true}).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
	}

	userRepo := repository.NewUserRepository(db)
	pondRepo := repository.NewPondRepository(db)
	readingRepo := repository.NewReadingRepository(db)
	planRepo := repository.NewPlanRepository(db)
	execRepo := repository.NewExecutionRepository(db)
	restRepo := repository.NewRestrictionRepository(db)
	auditRepo := repository.NewAuditRepository(db)
	auditSvc := service.NewAuditService(auditRepo)
	authSvc := service.NewAuthService(userRepo, "e2e-jwt-secret-at-least-32-chars-long", time.Hour)
	pondSvc := service.NewPondService(pondRepo, auditSvc)
	restSvc := service.NewRestrictionService(restRepo, pondRepo, readingRepo, auditSvc)
	readingSvc := service.NewReadingService(readingRepo, pondRepo, restSvc, auditSvc)
	planSvc := service.NewPlanService(planRepo, pondRepo, readingRepo, restRepo, auditSvc)
	execSvc := service.NewExecutionService(execRepo, planRepo, pondRepo, readingRepo, restRepo, auditSvc)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.RequestID(), middleware.Recovery())
	api := engine.Group("/api")
	api.POST("/auth/login", handler.NewAuthHandler(authSvc).Login)
	protected := api.Group("")
	protected.Use(middleware.AuthRequired(authSvc))
	h := struct {
		ponds        *handler.PondHandler
		readings     *handler.ReadingHandler
		plans        *handler.PlanHandler
		executions   *handler.ExecutionHandler
		restrictions *handler.RestrictionHandler
	}{
		handler.NewPondHandler(pondSvc), handler.NewReadingHandler(readingSvc),
		handler.NewPlanHandler(planSvc), handler.NewExecutionHandler(execSvc),
		handler.NewRestrictionHandler(restSvc),
	}
	protected.GET("/ponds", h.ponds.List)
	protected.GET("/ponds/:id/restriction", h.restrictions.ActiveByPond)
	pondW := protected.Group("/ponds")
	pondW.Use(middleware.RequireRoles("admin", "manager"))
	pondW.POST("", h.ponds.Create)
	readingW := protected.Group("/readings")
	readingW.Use(middleware.RequireRoles("admin", "manager", "operator"))
	readingW.POST("", h.readings.Create)
	planW := protected.Group("/plans")
	planW.Use(middleware.RequireRoles("admin", "manager", "operator"))
	planW.POST("", h.plans.Create)
	planW.PATCH("/:id/submit", h.plans.Submit)
	planReview := protected.Group("/plans")
	planReview.Use(middleware.RequireRoles("admin", "manager"))
	planReview.PATCH("/:id/approve", h.plans.Approve)
	execW := protected.Group("/executions")
	execW.Use(middleware.RequireRoles("admin", "manager", "operator"))
	execW.POST("", h.executions.Create)
	restDispose := protected.Group("/restrictions")
	restDispose.Use(middleware.RequireRoles("admin", "manager", "operator"))
	restDispose.PATCH("/:id/dispose", h.restrictions.Dispose)
	restRelease := protected.Group("/restrictions")
	restRelease.Use(middleware.RequireRoles("admin", "manager"))
	restRelease.PATCH("/:id/release", h.restrictions.Release)

	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)

	env := &e2eEnv{t: t, server: server, db: db, tokens: map[string]string{}}
	for _, u := range users {
		env.tokens[u.username] = env.login(u.username, u.password)
	}

	resp, envelope := env.do("POST", "/api/ponds", env.tokens["manager"], map[string]any{
		"code": "P-E2E-01", "name": "端到端闸门塘", "species": "石斑鱼", "areaSquareMeters": 2000,
		"capacityKg": 20000, "growthStage": "成长期", "status": "active", "manager": "塘主管", "notes": "",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create pond status = %d", resp.StatusCode)
	}
	pondData, _ := envelope["data"].(map[string]any)
	if pondData == nil {
		t.Fatalf("create pond response missing data: %#v", envelope)
	}
	env.pondID = uint(pondData["id"].(float64))
	return env
}

func (e *e2eEnv) login(username, password string) string {
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	resp, err := http.Post(e.server.URL+"/api/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		e.t.Fatalf("login: %v", err)
	}
	defer resp.Body.Close()
	var out struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		e.t.Fatalf("decode login: %v", err)
	}
	if out.Data.Token == "" {
		e.t.Fatalf("empty token for %s", username)
	}
	return out.Data.Token
}

func (e *e2eEnv) do(method, path, token string, payload any) (*http.Response, map[string]any) {
	var reader *bytes.Reader
	if payload != nil {
		raw, _ := json.Marshal(payload)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, e.server.URL+path, reader)
	if err != nil {
		e.t.Fatalf("request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatalf("do request: %v", err)
	}
	var envelope map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&envelope)
	resp.Body.Close()
	return resp, envelope
}

func errMessage(envelope map[string]any) string {
	if errBlock, ok := envelope["error"].(map[string]any); ok {
		if msg, ok := errBlock["message"].(string); ok {
			return msg
		}
	}
	return ""
}

func readingPayload(pondID uint, oxygen, ammonia float64, at time.Time) map[string]any {
	return map[string]any{
		"pondId": pondID, "dissolvedOxygen": oxygen, "temperature": 26.0, "ph": 7.4,
		"ammonia": ammonia, "turbidity": 20.0, "measuredAt": at.UTC().Format(time.RFC3339), "source": "manual",
	}
}

func (e *e2eEnv) mustCreatePlan(name string, token string, start, end time.Time) uint {
	resp, envelope := e.do("POST", "/api/plans", token, map[string]any{
		"pondId": e.pondID, "name": name, "dailyAmountKg": 40, "frequencyPerDay": 2, "feedType": "配合饲料",
		"targetGrowthStage": "成长期", "minOxygen": 5,
		"startDate": start.UTC().Format(time.RFC3339), "endDate": end.UTC().Format(time.RFC3339),
		"rationale": "端到端测试计划，水质依据充分且目标明确",
	})
	if resp.StatusCode != http.StatusCreated {
		e.t.Fatalf("create plan status = %d", resp.StatusCode)
	}
	data, _ := envelope["data"].(map[string]any)
	if data == nil {
		e.t.Fatalf("create plan missing data: %#v", envelope)
	}
	return uint(data["id"].(float64))
}

// 完整 HTTP 链路：严重读数自动建闸 → 批准/新执行被 409 拦截 → viewer 无法处置（403）
// → 操作员处置 → 无后续正常读数时主管无法解除（409）→ 正常读数后主管解除 → 流程恢复。
func TestFeedingSafetyGateEndToEnd(t *testing.T) {
	env := setupE2E(t)
	now := time.Now().UTC()
	op, mgr, viewer := env.tokens["operator"], env.tokens["manager"], env.tokens["viewer"]

	// 闸门建立前的正常基线读数与一条已批准计划。
	resp, _ := env.do("POST", "/api/readings", op, readingPayload(env.pondID, 7.0, 0.1, now.Add(-2*time.Hour)))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("baseline reading status = %d", resp.StatusCode)
	}
	planID := env.mustCreatePlan("基线投喂计划", op, now.Add(-time.Hour), now.Add(72*time.Hour))
	resp, _ = env.do("PATCH", "/api/plans/"+itoa(planID)+"/submit", op, map[string]string{"reason": "提交审核"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("submit status = %d", resp.StatusCode)
	}
	resp, _ = env.do("PATCH", "/api/plans/"+itoa(planID)+"/approve", mgr, map[string]string{"reason": "水质正常同意"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("baseline approve status = %d", resp.StatusCode)
	}

	// 严重读数 → 自动停喂，前端通过 /ponds/:id/restriction 可读到状态与责任人。
	resp, _ = env.do("POST", "/api/readings", op, readingPayload(env.pondID, 2.2, 1.5, now.Add(-90*time.Minute)))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("critical reading status = %d", resp.StatusCode)
	}
	resp, envelope := env.do("GET", "/api/ponds/"+itoa(env.pondID)+"/restriction", viewer, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("active restriction lookup status = %d", resp.StatusCode)
	}
	data, _ := envelope["data"].(map[string]any)
	if data == nil || data["status"] != "active" {
		t.Fatalf("expected active restriction, got %#v", envelope["data"])
	}
	if data["triggeredBy"] != "值班操作员" {
		t.Fatalf("triggeredBy = %v", data["triggeredBy"])
	}
	restrictionID := uint(data["id"].(float64))

	// 闸门拦截：新建执行 409。
	resp, envelope = env.do("POST", "/api/executions", op, map[string]any{
		"pondId": env.pondID, "feedingPlanId": planID,
		"scheduledAt":     now.Add(2 * time.Hour).UTC().Format(time.RFC3339),
		"plannedAmountKg": 10, "weather": "晴朗",
	})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("execution create status = %d, want 409", resp.StatusCode)
	}
	if msg := errMessage(envelope); !contains(msg, "停喂限制") {
		t.Fatalf("execution block message = %q", msg)
	}

	// 闸门拦截：新计划批准 409。
	planID2 := env.mustCreatePlan("闸门期计划", op, now.Add(-time.Hour), now.Add(72*time.Hour))
	resp, _ = env.do("PATCH", "/api/plans/"+itoa(planID2)+"/submit", op, map[string]string{"reason": "提交"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("submit plan2 status = %d", resp.StatusCode)
	}
	resp, envelope = env.do("PATCH", "/api/plans/"+itoa(planID2)+"/approve", mgr, map[string]string{"reason": "尝试批准"})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("approve under restriction status = %d, want 409", resp.StatusCode)
	}

	// RBAC：观察员不能提交处置说明。
	resp, _ = env.do("PATCH", "/api/restrictions/"+itoa(restrictionID)+"/dispose", viewer, map[string]string{"dispositionNote": "越权处置"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer dispose status = %d, want 403", resp.StatusCode)
	}

	// 操作员提交处置说明。
	resp, _ = env.do("PATCH", "/api/restrictions/"+itoa(restrictionID)+"/dispose", op, map[string]string{"dispositionNote": "已开启全部增氧机并换水 30%，氨氮持续回落"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dispose status = %d", resp.StatusCode)
	}
	// 重复处置失败，不产生重复流转。
	resp, envelope = env.do("PATCH", "/api/restrictions/"+itoa(restrictionID)+"/dispose", op, map[string]string{"dispositionNote": "再次提交处置"})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate dispose status = %d, want 409", resp.StatusCode)
	}

	// 处置后仍无后续正常读数：主管解除失败。
	resp, envelope = env.do("PATCH", "/api/restrictions/"+itoa(restrictionID)+"/release", mgr, map[string]string{"releaseBasis": "尚未复测就解除"})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("premature release status = %d, want 409", resp.StatusCode)
	}
	// RBAC：操作员不能解除。
	resp, _ = env.do("PATCH", "/api/restrictions/"+itoa(restrictionID)+"/release", op, map[string]string{"releaseBasis": "操作员自行解除"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("operator release status = %d, want 403", resp.StatusCode)
	}

	// 录入触发时间之后的正常读数，主管填写复核依据后解除。
	resp, _ = env.do("POST", "/api/readings", op, readingPayload(env.pondID, 7.1, 0.06, now.Add(-20*time.Minute)))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("recovery reading status = %d", resp.StatusCode)
	}
	resp, envelope = env.do("PATCH", "/api/restrictions/"+itoa(restrictionID)+"/release", mgr, map[string]string{"releaseBasis": "复测溶解氧 7.1、氨氮 0.06，现场摄食恢复，同意解除"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("release status = %d", resp.StatusCode)
	}
	resp, envelope = env.do("GET", "/api/ponds/"+itoa(env.pondID)+"/restriction", viewer, nil)
	if envelope["data"] != nil {
		t.Fatalf("after release active restriction should be null, got %#v", envelope["data"])
	}

	// 正常投喂与审计流程恢复：批准与执行均成功。
	resp, _ = env.do("PATCH", "/api/plans/"+itoa(planID2)+"/approve", mgr, map[string]string{"reason": "复测正常，恢复投喂"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("approve after release status = %d", resp.StatusCode)
	}
	resp, _ = env.do("POST", "/api/executions", op, map[string]any{
		"pondId": env.pondID, "feedingPlanId": planID2,
		"scheduledAt":     now.Add(3 * time.Hour).UTC().Format(time.RFC3339),
		"plannedAmountKg": 10, "weather": "晴朗",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("execution after release status = %d, want 201", resp.StatusCode)
	}

	// 审计链完整：restrict/dispose/release 各一条。
	var actions []struct {
		Action string
	}
	if err := env.db.Model(&model.AuditLog{}).Where("entity_type = ?", "feeding_restriction").Order("id").Find(&actions).Error; err != nil {
		t.Fatalf("query restriction audits: %v", err)
	}
	got := ""
	for _, a := range actions {
		got += a.Action + ","
	}
	if got != "restrict,dispose,release," {
		t.Fatalf("restriction audit chain = %q, want restrict,dispose,release", got)
	}
}

func itoa(id uint) string {
	return strconv.FormatUint(uint64(id), 10)
}

func contains(haystack, needle string) bool {
	return bytes.Contains([]byte(haystack), []byte(needle))
}

var e2eDSNDBNamePattern = regexp.MustCompile(`dbname=\S+`)

func withDatabaseName(dsn, name string) string {
	if e2eDSNDBNamePattern.MatchString(dsn) {
		return e2eDSNDBNamePattern.ReplaceAllString(dsn, "dbname="+name)
	}
	return strings.TrimSpace(dsn) + " dbname=" + name
}

func openSandboxDatabase(t *testing.T, baseDSN string) string {
	t.Helper()
	admin, err := gorm.Open(postgres.Open(baseDSN), &gorm.Config{})
	if err != nil {
		t.Fatalf("open maintenance database: %v", err)
	}
	name := fmt.Sprintf("aqua_e2e_%d_%d", time.Now().UnixNano(), rand.Intn(1_000_000))
	if err := admin.Exec(fmt.Sprintf(`CREATE DATABASE %s`, name)).Error; err != nil {
		t.Fatalf("create sandbox database: %v", err)
	}
	if sqlDB, err := admin.DB(); err == nil {
		_ = sqlDB.Close()
	}
	return name
}
