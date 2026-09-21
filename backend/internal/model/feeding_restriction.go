package model

import (
	"aquaculture-water-feeding-control/backend/internal/constants"
	"time"
)

// FeedingRestriction 记录单个养殖池因严重水质异常建立的停喂限制。
// 状态机：active（停喂中，阻断批准与新建执行）→ disposed（操作员已提交处置说明，等待正常读数复核）
// → released（主管依据后续正常读数填写复核依据后解除）。
type FeedingRestriction struct {
	Base
	PondID            uint                        `gorm:"not null;index" json:"pondId"`
	Pond              *Pond                       `gorm:"constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"pond,omitempty"`
	Status            constants.RestrictionStatus `gorm:"size:20;not null;index" json:"status"`
	TriggerReadingID  uint                        `gorm:"not null" json:"triggerReadingId"`
	TriggerReading    *WaterReading               `gorm:"foreignKey:TriggerReadingID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"triggerReading,omitempty"`
	TriggerRiskLevel  constants.RiskLevel         `gorm:"size:20;not null" json:"triggerRiskLevel"`
	TriggerReason     string                      `gorm:"type:text;not null" json:"triggerReason"`
	TriggerMeasuredAt time.Time                   `gorm:"not null;index" json:"triggerMeasuredAt"`
	TriggeredBy       string                      `gorm:"size:80;not null" json:"triggeredBy"`
	DispositionNote   string                      `gorm:"type:text" json:"dispositionNote"`
	DisposedBy        string                      `gorm:"size:80" json:"disposedBy"`
	DisposedAt        *time.Time                  `json:"disposedAt"`
	NormalReadingID   *uint                       `json:"normalReadingId"`
	NormalReading     *WaterReading               `gorm:"foreignKey:NormalReadingID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"normalReading,omitempty"`
	ReleaseBasis      string                      `gorm:"type:text" json:"releaseBasis"`
	ReleasedBy        string                      `gorm:"size:80" json:"releasedBy"`
	ReleasedAt        *time.Time                  `json:"releasedAt"`
}
