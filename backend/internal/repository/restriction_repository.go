package repository

import (
	"aquaculture-water-feeding-control/backend/internal/constants"
	"aquaculture-water-feeding-control/backend/internal/dto"
	"aquaculture-water-feeding-control/backend/internal/model"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// OpenRestrictionStatuses 表示仍然关闭投喂闸门的状态：处置中或已处置待解除均不得放行。
var OpenRestrictionStatuses = []constants.RestrictionStatus{
	constants.RestrictionActive,
	constants.RestrictionHandled,
}

type RestrictionRepository struct {
	db *gorm.DB
}

func NewRestrictionRepository(db *gorm.DB) *RestrictionRepository {
	return &RestrictionRepository{db: db}
}

func (r *RestrictionRepository) List(query dto.PageQuery, pondID uint, status constants.RestrictionStatus) ([]model.FeedingRestriction, int64, error) {
	base := r.db.Model(&model.FeedingRestriction{})
	if pondID > 0 {
		base = base.Where("pond_id = ?", pondID)
	}
	if status != "" {
		base = base.Where("status = ?", status)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var restrictions []model.FeedingRestriction
	err := base.
		Preload("Pond").
		Preload("TriggerReading").
		Order("created_at DESC").
		Offset((query.Page - 1) * query.PageSize).
		Limit(query.PageSize).
		Find(&restrictions).Error
	return restrictions, total, err
}

func (r *RestrictionRepository) Get(id uint) (model.FeedingRestriction, error) {
	var restriction model.FeedingRestriction
	err := r.db.Preload("Pond").Preload("TriggerReading").First(&restriction, id).Error
	return restriction, err
}

func (r *RestrictionRepository) GetForUpdate(id uint) (model.FeedingRestriction, error) {
	var restriction model.FeedingRestriction
	err := r.db.
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Preload("Pond").
		Preload("TriggerReading").
		First(&restriction, id).Error
	return restriction, err
}

func (r *RestrictionRepository) Create(restriction *model.FeedingRestriction) error {
	return r.db.Create(restriction).Error
}

// ErrRestrictionAlreadyExists 表示同一养殖池已存在未解除的停喂限制（唯一索引冲突）。
var ErrRestrictionAlreadyExists = errors.New("feeding restriction already exists for pond")

// CreateIfAbsent 在同一养殖池尚无未解除限制时插入新闸门。
// 该方法设计用于严重读数创建事务内部：并发情况下若唯一部分索引冲突，
// 只回滚到保存点（避免污染外层读数事务），并返回 ErrRestrictionAlreadyExists，
// 由服务层按“限制已存在”幂等处理，绝不让失败放行或产生重复记录。
func (r *RestrictionRepository) CreateIfAbsent(restriction *model.FeedingRestriction) (bool, error) {
	const savepoint = "sp_restriction_create"
	if err := r.db.Exec("SAVEPOINT " + savepoint).Error; err != nil {
		return false, err
	}
	if err := r.db.Create(restriction).Error; err != nil {
		if isUniqueViolation(err) {
			if rbErr := r.db.Exec("ROLLBACK TO SAVEPOINT " + savepoint).Error; rbErr != nil {
				return false, rbErr
			}
			return false, ErrRestrictionAlreadyExists
		}
		_ = r.db.Exec("ROLLBACK TO SAVEPOINT " + savepoint).Error
		return false, err
	}
	if err := r.db.Exec("RELEASE SAVEPOINT " + savepoint).Error; err != nil {
		return false, err
	}
	return true, nil
}

// isUniqueViolation 判断是否为唯一约束冲突：
// PostgreSQL SQLSTATE 23505，或测试环境使用的 SQLite UNIQUE constraint。
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint failed") || strings.Contains(message, "duplicate key")
}

func (r *RestrictionRepository) Save(restriction *model.FeedingRestriction) error {
	return r.db.Save(restriction).Error
}

// OpenForPond 返回养殖池当前尚未解除的停喂限制；没有则返回 gorm.ErrRecordNotFound。
func (r *RestrictionRepository) OpenForPond(pondID uint) (model.FeedingRestriction, error) {
	var restriction model.FeedingRestriction
	err := r.db.
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Preload("TriggerReading").
		Where("pond_id = ? AND status IN ?", pondID, OpenRestrictionStatuses).
		Order("created_at DESC").
		First(&restriction).Error
	return restriction, err
}

// OpenByPondIDs 批量查询多个养殖池的未解除限制，供列表页面展示闸门状态。
func (r *RestrictionRepository) OpenByPondIDs(pondIDs []uint) (map[uint]model.FeedingRestriction, error) {
	result := make(map[uint]model.FeedingRestriction)
	if len(pondIDs) == 0 {
		return result, nil
	}
	var restrictions []model.FeedingRestriction
	if err := r.db.
		Preload("TriggerReading").
		Where("pond_id IN ? AND status IN ?", pondIDs, OpenRestrictionStatuses).
		Order("created_at DESC").
		Find(&restrictions).Error; err != nil {
		return nil, err
	}
	// 部分唯一索引保证每塘至多一条；ORDER BY 仅作防御性兜底，保留最新一条。
	for _, restriction := range restrictions {
		if _, exists := result[restriction.PondID]; !exists {
			result[restriction.PondID] = restriction
		}
	}
	return result, nil
}
