package model

import (
	"aquaculture-water-feeding-control/backend/internal/constants"
	"time"
)

// FeedingRestriction 是同一养殖池出现严重水质异常后建立的停喂安全闸门。
// 生命周期：active（停喂中）→ handled（操作员已提交处置说明，等待主管解除）→ released（主管复核后解除）。
// 任意时刻同一养殖池至多存在一条 active/handled 记录（由部分唯一索引保证，见 database.Open）。
type FeedingRestriction struct {
	Base
	PondID uint  `gorm:"not null;index" json:"pondId"`
	Pond   *Pond `gorm:"constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"pond,omitempty"`

	// 触发闸门的严重水质读数。
	TriggerReadingID uint          `gorm:"not null;index" json:"triggerReadingId"`
	TriggerReading   *WaterReading `gorm:"constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"triggerReading,omitempty"`
	TriggerReason    string        `gorm:"type:text;not null" json:"triggerReason"`

	Status constants.RestrictionStatus `gorm:"size:20;not null;index" json:"status"`

	// 操作员处置说明（active -> handled）。
	HandleNote string     `gorm:"type:text" json:"handleNote"`
	HandledBy  string     `gorm:"size:80" json:"handledBy"`
	HandledAt  *time.Time `json:"handledAt"`

	// 主管解除复核依据（handled -> released）。
	ReleaseNote string     `gorm:"type:text" json:"releaseNote"`
	ReleasedBy  string     `gorm:"size:80" json:"releasedBy"`
	ReleasedAt  *time.Time `json:"releasedAt"`
}
