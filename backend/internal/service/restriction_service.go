package service

import (
	"aquaculture-water-feeding-control/backend/internal/constants"
	"aquaculture-water-feeding-control/backend/internal/dto"
	"aquaculture-water-feeding-control/backend/internal/model"
	"aquaculture-water-feeding-control/backend/internal/repository"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

type RestrictionService struct {
	repo          *repository.RestrictionRepository
	ponds         *repository.PondRepository
	readings      *repository.ReadingRepository
	audit         *AuditService
	transactional bool
}

func (s *RestrictionService) withinTransaction(fn func(*RestrictionService) error) error {
	return s.audit.WithinTransaction(func(tx *gorm.DB, audit *AuditService) error {
		scoped := &RestrictionService{
			repo: repository.NewRestrictionRepository(tx), ponds: repository.NewPondRepository(tx),
			readings: repository.NewReadingRepository(tx), audit: audit, transactional: true,
		}
		return fn(scoped)
	})
}

func NewRestrictionService(repo *repository.RestrictionRepository, ponds *repository.PondRepository, readings *repository.ReadingRepository, audit *AuditService) *RestrictionService {
	return &RestrictionService{repo: repo, ponds: ponds, readings: readings, audit: audit}
}

// NewRestrictionServiceTx 在已有事务内构造使用同一 *gorm.DB 的限制服务。
func NewRestrictionServiceTx(tx *gorm.DB, audit *AuditService) *RestrictionService {
	return &RestrictionService{
		repo:          repository.NewRestrictionRepository(tx),
		ponds:         repository.NewPondRepository(tx),
		readings:      repository.NewReadingRepository(tx),
		audit:         audit,
		transactional: true,
	}
}

func (s *RestrictionService) List(query dto.PageQuery, pondID uint, status string) (dto.PageResult[model.FeedingRestriction], error) {
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

// ActiveForPond 返回养殖池当前生效（active/disposed）的停喂限制；不存在时返回 false。
func (s *RestrictionService) ActiveForPond(pondID uint) (model.FeedingRestriction, bool, error) {
	restriction, err := s.repo.ActiveForPond(pondID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.FeedingRestriction{}, false, nil
	}
	if err != nil {
		return model.FeedingRestriction{}, false, WrapError(CodeInternal, "查询停喂限制失败", err)
	}
	return restriction, true, nil
}

// EstablishFromCriticalReading 在严重水质读数创建事务内被调用：
// 同一池塘已有生效限制时幂等返回该记录（不新建、不重复审计），否则建立停喂限制。
// 调用方必须已经持有该池塘的行锁（GetForUpdate），以串行化并发的严重读数录入。
func (s *RestrictionService) EstablishFromCriticalReading(pondID, readingID uint, level constants.RiskLevel, reason string, measuredAt time.Time, actor Actor) (model.FeedingRestriction, error) {
	existing, err := s.repo.ActiveForPondForUpdate(pondID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.FeedingRestriction{}, WrapError(CodeInternal, "查询停喂限制失败", err)
	}
	restriction := model.FeedingRestriction{
		PondID: pondID, Status: constants.RestrictionActive, TriggerReadingID: readingID,
		TriggerRiskLevel: level, TriggerReason: reason, TriggerMeasuredAt: measuredAt.UTC(),
		TriggeredBy: actorName(actor),
	}
	if err := s.repo.Create(&restriction); err != nil {
		// 部分唯一索引兜底：并发下已有生效限制时，不阻断读数录入，返回既有记录。
		if isUniqueViolation(err) {
			existing, retryErr := s.repo.ActiveForPondForUpdate(pondID)
			if retryErr == nil {
				return existing, nil
			}
			return model.FeedingRestriction{}, WrapError(CodeInternal, "建立停喂限制失败", retryErr)
		}
		return model.FeedingRestriction{}, WrapError(CodeInternal, "建立停喂限制失败", err)
	}
	if err := s.audit.Record(actor, "restrict", "feeding_restriction", restriction.ID, nil, restriction, reason); err != nil {
		return model.FeedingRestriction{}, err
	}
	return restriction, nil
}

func (s *RestrictionService) Dispose(id uint, note string, actor Actor) (model.FeedingRestriction, error) {
	if !s.transactional {
		var result model.FeedingRestriction
		err := s.withinTransaction(func(scoped *RestrictionService) error {
			var inner error
			result, inner = scoped.Dispose(id, note, actor)
			return inner
		})
		return result, err
	}
	restriction, err := s.Get(id)
	if err != nil {
		return model.FeedingRestriction{}, err
	}
	switch restriction.Status {
	case constants.RestrictionDisposed:
		return model.FeedingRestriction{}, NewError(CodeConflict, "该停喂限制已提交处置说明，不能重复提交")
	case constants.RestrictionReleased:
		return model.FeedingRestriction{}, NewError(CodeConflict, "该停喂限制已解除，无需再提交处置")
	}
	before := restriction
	now := time.Now().UTC()
	restriction.Status = constants.RestrictionDisposed
	restriction.DispositionNote = strings.TrimSpace(note)
	restriction.DisposedBy = actorName(actor)
	restriction.DisposedAt = &now
	if err := s.repo.Save(&restriction); err != nil {
		return model.FeedingRestriction{}, WrapError(CodeInternal, "提交处置说明失败", err)
	}
	if err := s.audit.Record(actor, "dispose", "feeding_restriction", restriction.ID, before, restriction, note); err != nil {
		return model.FeedingRestriction{}, err
	}
	return restriction, nil
}

func (s *RestrictionService) Release(id uint, basis string, actor Actor) (model.FeedingRestriction, error) {
	if !s.transactional {
		var result model.FeedingRestriction
		err := s.withinTransaction(func(scoped *RestrictionService) error {
			var inner error
			result, inner = scoped.Release(id, basis, actor)
			return inner
		})
		return result, err
	}
	restriction, err := s.Get(id)
	if err != nil {
		return model.FeedingRestriction{}, err
	}
	if restriction.Status == constants.RestrictionReleased {
		return model.FeedingRestriction{}, NewError(CodeConflict, "该停喂限制已解除，不能重复解除")
	}
	if restriction.Status != constants.RestrictionDisposed {
		return model.FeedingRestriction{}, NewError(CodeConflict, "需先由操作员提交处置说明后才能解除停喂限制")
	}
	latest, err := s.readings.LatestForPond(restriction.PondID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.FeedingRestriction{}, NewError(CodeConflict, "解除前必须录入触发异常之后的正常水质读数")
	}
	if err != nil {
		return model.FeedingRestriction{}, WrapError(CodeInternal, "查询最新水质读数失败", err)
	}
	if !latest.MeasuredAt.After(restriction.TriggerMeasuredAt) {
		return model.FeedingRestriction{}, NewError(CodeConflict, "解除前必须录入触发异常之后采集的新读数")
	}
	if latest.RiskLevel != constants.RiskNormal {
		return model.FeedingRestriction{}, NewError(CodeConflict, "最新水质读数仍未恢复正常，不能解除停喂限制")
	}
	before := restriction
	now := time.Now().UTC()
	restriction.Status = constants.RestrictionReleased
	restriction.NormalReadingID = &latest.ID
	restriction.ReleaseBasis = strings.TrimSpace(basis)
	restriction.ReleasedBy = actorName(actor)
	restriction.ReleasedAt = &now
	if err := s.repo.Save(&restriction); err != nil {
		return model.FeedingRestriction{}, WrapError(CodeInternal, "解除停喂限制失败", err)
	}
	if err := s.audit.Record(actor, "release", "feeding_restriction", restriction.ID, before, restriction, basis); err != nil {
		return model.FeedingRestriction{}, err
	}
	return restriction, nil
}

func actorName(actor Actor) string {
	if actor.DisplayName != "" {
		return actor.DisplayName
	}
	return actor.Username
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
