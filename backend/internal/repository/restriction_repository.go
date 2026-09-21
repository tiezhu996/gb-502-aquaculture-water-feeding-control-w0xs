package repository

import (
	"aquaculture-water-feeding-control/backend/internal/dto"
	"aquaculture-water-feeding-control/backend/internal/model"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RestrictionRepository struct {
	db *gorm.DB
}

func NewRestrictionRepository(db *gorm.DB) *RestrictionRepository {
	return &RestrictionRepository{db: db}
}

func (r *RestrictionRepository) List(query dto.PageQuery, pondID uint, status string) ([]model.FeedingRestriction, int64, error) {
	base := r.db.Model(&model.FeedingRestriction{})
	if pondID > 0 {
		base = base.Where("pond_id = ?", pondID)
	}
	if statuses := parseRestrictionStatuses(status); len(statuses) > 0 {
		base = base.Where("status IN ?", statuses)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []model.FeedingRestriction
	err := base.Preload("Pond").Order("created_at DESC").Offset((query.Page - 1) * query.PageSize).Limit(query.PageSize).Find(&items).Error
	return items, total, err
}

func (r *RestrictionRepository) Get(id uint) (model.FeedingRestriction, error) {
	var restriction model.FeedingRestriction
	err := r.db.Preload("Pond").Preload("TriggerReading").Preload("NormalReading").First(&restriction, id).Error
	return restriction, err
}

func (r *RestrictionRepository) GetForUpdate(id uint) (model.FeedingRestriction, error) {
	var restriction model.FeedingRestriction
	err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Preload("Pond").Preload("TriggerReading").Preload("NormalReading").First(&restriction, id).Error
	return restriction, err
}

// ActiveForPondForUpdate 在事务内查询池塘当前生效的停喂限制并加行锁。
func (r *RestrictionRepository) ActiveForPondForUpdate(pondID uint) (model.FeedingRestriction, error) {
	var restriction model.FeedingRestriction
	err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("pond_id = ? AND status IN ?", pondID, []string{"active", "disposed"}).
		Order("created_at DESC").First(&restriction).Error
	return restriction, err
}

func (r *RestrictionRepository) ActiveForPond(pondID uint) (model.FeedingRestriction, error) {
	var restriction model.FeedingRestriction
	err := r.db.Where("pond_id = ? AND status IN ?", pondID, []string{"active", "disposed"}).
		Order("created_at DESC").First(&restriction).Error
	return restriction, err
}

func (r *RestrictionRepository) Create(restriction *model.FeedingRestriction) error {
	return r.db.Create(restriction).Error
}

func (r *RestrictionRepository) Save(restriction *model.FeedingRestriction) error {
	return r.db.Save(restriction).Error
}

// CountForPond 统计池塘的限制记录数（含已解除），用于池塘删除依赖检查。
func (r *RestrictionRepository) CountForPond(pondID uint) (int64, error) {
	var count int64
	err := r.db.Model(&model.FeedingRestriction{}).Where("pond_id = ?", pondID).Count(&count).Error
	return count, err
}

// parseRestrictionStatuses 支持逗号分隔的多状态过滤（如 "active,disposed"），忽略非法取值。
func parseRestrictionStatuses(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	allowed := map[string]struct{}{"active": {}, "disposed": {}, "released": {}}
	statuses := make([]string, 0, 3)
	for _, part := range strings.Split(raw, ",") {
		status := strings.TrimSpace(part)
		if _, ok := allowed[status]; ok {
			statuses = append(statuses, status)
		}
	}
	return statuses
}
