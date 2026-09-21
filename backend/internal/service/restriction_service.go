package service

import (
	"aquaculture-water-feeding-control/backend/internal/constants"
	"aquaculture-water-feeding-control/backend/internal/dto"
	"aquaculture-water-feeding-control/backend/internal/model"
	"aquaculture-water-feeding-control/backend/internal/repository"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

type RestrictionService struct {
	repo          *repository.RestrictionRepository
	ponds         *repository.PondRepository
	readings      *repository.ReadingRepository
	audit         *AuditService
	transactional bool
}

func NewRestrictionService(
	repo *repository.RestrictionRepository,
	ponds *repository.PondRepository,
	readings *repository.ReadingRepository,
	audit *AuditService,
) *RestrictionService {
	return &RestrictionService{repo: repo, ponds: ponds, readings: readings, audit: audit}
}

func (s *RestrictionService) withinTransaction(fn func(*RestrictionService) error) error {
	return s.audit.WithinTransaction(func(tx *gorm.DB, audit *AuditService) error {
		scoped := &RestrictionService{
			repo:          repository.NewRestrictionRepository(tx),
			ponds:         repository.NewPondRepository(tx),
			readings:      repository.NewReadingRepository(tx),
			audit:         audit,
			transactional: true,
		}
		return fn(scoped)
	})
}

func (s *RestrictionService) List(query dto.PageQuery, pondID uint, status constants.RestrictionStatus) (dto.PageResult[model.FeedingRestriction], error) {
	query.Normalize()
	items, total, err := s.repo.List(query, pondID, status)
	if err != nil {
		return dto.PageResult[model.FeedingRestriction]{}, WrapError(CodeInternal, "查询停喂限制失败", err)
	}
	return dto.PageResult[model.FeedingRestriction]{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize}, nil
}

func (s *RestrictionService) Get(id uint) (model.FeedingRestriction, error) {
	var restriction model.FeedingRestriction
	var err error
	if s.transactional {
		restriction, err = s.repo.GetForUpdate(id)
	} else {
		restriction, err = s.repo.Get(id)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.FeedingRestriction{}, NewError(CodeNotFound, "停喂限制不存在")
	}
	if err != nil {
		return model.FeedingRestriction{}, WrapError(CodeInternal, "查询停喂限制失败", err)
	}
	return restriction, nil
}

// EnsureForCritical 在严重水质读数创建事务内被调用：自动为养殖池建立停喂限制。
// 重复或并发出现严重读数时，若闸门仍未解除则不重复建限，返回的 bool 表示本次是否实际新建。
// 调用方必须已经运行在事务中（transactional），以保证读数、闸门、审计同提交同回滚。
func (s *RestrictionService) EnsureForCritical(reading model.WaterReading, actor Actor) (model.FeedingRestriction, bool, error) {
	if !s.transactional {
		return model.FeedingRestriction{}, false, NewError(CodeInternal, "建立停喂限制必须在事务中执行")
	}
	if reading.RiskLevel != constants.RiskCritical {
		return model.FeedingRestriction{}, false, nil
	}
	// 行锁 + 部分唯一索引双重保护：仍有未解除闸门时不重复建限。
	if existing, err := s.repo.OpenForPond(reading.PondID); err == nil {
		return existing, false, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.FeedingRestriction{}, false, WrapError(CodeInternal, "检查停喂限制失败", err)
	}

	reason := strings.TrimSpace(reading.AlertMessage)
	if reason == "" {
		reason = "水质读数判定为严重异常，自动建立停喂限制"
	}
	restriction := model.FeedingRestriction{
		PondID:           reading.PondID,
		TriggerReadingID: reading.ID,
		TriggerReason:    reason,
		Status:           constants.RestrictionActive,
	}
	created, err := s.repo.CreateIfAbsent(&restriction)
	if err != nil {
		if errors.Is(err, repository.ErrRestrictionAlreadyExists) {
			existing, lookupErr := s.repo.OpenForPond(reading.PondID)
			if lookupErr != nil {
				return model.FeedingRestriction{}, false, WrapError(CodeInternal, "查询已有停喂限制失败", lookupErr)
			}
			return existing, false, nil
		}
		return model.FeedingRestriction{}, false, WrapError(CodeInternal, "建立停喂限制失败", err)
	}
	if !created {
		existing, lookupErr := s.repo.OpenForPond(reading.PondID)
		if lookupErr != nil {
			return model.FeedingRestriction{}, false, WrapError(CodeInternal, "查询已有停喂限制失败", lookupErr)
		}
		return existing, false, nil
	}
	if err := s.audit.Record(actor, "restrict", "feeding_restriction", restriction.ID, nil, restriction, reason); err != nil {
		return model.FeedingRestriction{}, false, err
	}
	return restriction, true, nil
}

// Handle 操作员提交现场处置说明：active -> handled。闸门仍保持关闭，等待主管解除。
func (s *RestrictionService) Handle(id uint, note string, actor Actor) (model.FeedingRestriction, error) {
	if !s.transactional {
		var result model.FeedingRestriction
		err := s.withinTransaction(func(scoped *RestrictionService) error {
			var inner error
			result, inner = scoped.Handle(id, note, actor)
			return inner
		})
		return result, err
	}
	restriction, err := s.Get(id)
	if err != nil {
		return model.FeedingRestriction{}, err
	}
	if restriction.Status != constants.RestrictionActive {
		return model.FeedingRestriction{}, NewError(CodeConflict, "当前停喂限制状态不允许提交处置说明（可能已处置或已解除）")
	}
	before := restriction
	now := time.Now().UTC()
	restriction.Status = constants.RestrictionHandled
	restriction.HandleNote = strings.TrimSpace(note)
	restriction.HandledBy = actorName(actor)
	restriction.HandledAt = &now
	if err := s.repo.Save(&restriction); err != nil {
		return model.FeedingRestriction{}, WrapError(CodeInternal, "提交处置说明失败", err)
	}
	if err := s.audit.Record(actor, "handle", "feeding_restriction", restriction.ID, before, restriction, note); err != nil {
		return model.FeedingRestriction{}, err
	}
	return restriction, nil
}

// Release 主管解除停喂限制：handled -> released。
// 前提：操作员已处置，且触发读数之后出现新的正常读数；主管必须填写复核依据。
func (s *RestrictionService) Release(id uint, reviewNote string, actor Actor) (model.FeedingRestriction, error) {
	if !s.transactional {
		var result model.FeedingRestriction
		err := s.withinTransaction(func(scoped *RestrictionService) error {
			var inner error
			result, inner = scoped.Release(id, reviewNote, actor)
			return inner
		})
		return result, err
	}
	restriction, err := s.Get(id)
	if err != nil {
		return model.FeedingRestriction{}, err
	}
	// 统一加锁顺序：先池塘行、后限制行，避免与批准/执行流程（均先锁池塘）形成 AB-BA 死锁。
	pond, err := s.ponds.GetForUpdate(restriction.PondID)
	if err != nil {
		return model.FeedingRestriction{}, WrapError(CodeInternal, "查询养殖池失败", err)
	}
	restriction, err = s.repo.GetForUpdate(id)
	if err != nil {
		return model.FeedingRestriction{}, WrapError(CodeInternal, "查询停喂限制失败", err)
	}
	if restriction.PondID != pond.ID {
		return model.FeedingRestriction{}, NewError(CodeConflict, "停喂限制与养殖池不匹配")
	}
	if restriction.Status == constants.RestrictionActive {
		return model.FeedingRestriction{}, NewError(CodeConflict, "操作员尚未提交处置说明，不能解除停喂限制")
	}
	if restriction.Status == constants.RestrictionReleased {
		return model.FeedingRestriction{}, NewError(CodeConflict, "停喂限制已解除，请勿重复操作")
	}
	if restriction.Status != constants.RestrictionHandled {
		return model.FeedingRestriction{}, NewError(CodeConflict, "当前停喂限制状态不允许解除")
	}

	latest, err := s.readings.LatestForPond(restriction.PondID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.FeedingRestriction{}, NewError(CodeConflict, "解除前必须先录入恢复正常的水质读数")
	}
	if err != nil {
		return model.FeedingRestriction{}, WrapError(CodeInternal, "查询最新水质读数失败", err)
	}
	if err := validateReleaseReading(latest, restriction.TriggerReadingID); err != nil {
		return model.FeedingRestriction{}, err
	}

	before := restriction
	now := time.Now().UTC()
	restriction.Status = constants.RestrictionReleased
	restriction.ReleaseNote = strings.TrimSpace(reviewNote)
	restriction.ReleasedBy = actorName(actor)
	restriction.ReleasedAt = &now
	if err := s.repo.Save(&restriction); err != nil {
		return model.FeedingRestriction{}, WrapError(CodeInternal, "解除停喂限制失败", err)
	}
	if err := s.audit.Record(actor, "release", "feeding_restriction", restriction.ID, before, restriction, reviewNote); err != nil {
		return model.FeedingRestriction{}, err
	}
	return restriction, nil
}

// RequireFeedingAllowed 是投喂安全闸门的统一校验：池塘存在未解除限制时阻止放行。
// 必须在已持有池塘行锁的事务内调用（批准/安排执行流程均先 GetForUpdate 池塘）。
func (s *RestrictionService) RequireFeedingAllowed(pondID uint, action string) error {
	restriction, err := s.repo.OpenForPond(pondID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return WrapError(CodeInternal, "检查停喂限制失败", err)
	}
	return conflictForFeeding(restriction, action)
}

func validateReleaseReading(latest model.WaterReading, triggerReadingID uint) error {
	if latest.RiskLevel != constants.RiskNormal {
		return NewError(CodeConflict, "最新水质读数尚未恢复正常，不能解除停喂限制")
	}
	if latest.ID <= triggerReadingID {
		return NewError(CodeConflict, "必须在严重异常处置后重新采集到正常读数，才能解除停喂限制")
	}
	return nil
}

func conflictForFeeding(restriction model.FeedingRestriction, action string) error {
	switch restriction.Status {
	case constants.RestrictionHandled:
		return NewError(CodeConflict, "该养殖池停喂限制已提交处置但尚未经主管解除，"+action+"被安全闸门阻止")
	default:
		return NewError(CodeConflict, "该养殖池存在严重水质异常停喂限制，且尚未提交处置说明，"+action+"被安全闸门阻止")
	}
}

func actorName(actor Actor) string {
	if actor.DisplayName != "" {
		return actor.DisplayName
	}
	return actor.Username
}
