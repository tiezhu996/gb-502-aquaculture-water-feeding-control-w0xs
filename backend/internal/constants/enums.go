package constants

type PondStatus string

const (
	PondStatusActive     PondStatus = "active"
	PondStatusQuarantine PondStatus = "quarantine"
	PondStatusClosed     PondStatus = "closed"
)

func (s ExecutionStatus) Valid() bool {
	return s == ExecutionScheduled || s == ExecutionRunning || s == ExecutionCompleted || s == ExecutionCancelled
}

func (s ExecutionStatus) CanTransitionTo(next ExecutionStatus) bool {
	switch s {
	case ExecutionScheduled:
		return next == ExecutionScheduled || next == ExecutionRunning || next == ExecutionCompleted || next == ExecutionCancelled
	case ExecutionRunning:
		return next == ExecutionRunning || next == ExecutionCompleted || next == ExecutionCancelled
	default:
		return false
	}
}

func (s PondStatus) Valid() bool {
	return s == PondStatusActive || s == PondStatusQuarantine || s == PondStatusClosed
}

type PlanStatus string

const (
	PlanStatusDraft    PlanStatus = "draft"
	PlanStatusPending  PlanStatus = "pending"
	PlanStatusApproved PlanStatus = "approved"
	PlanStatusExecuted PlanStatus = "executed"
)

func (s PlanStatus) Valid() bool {
	return s == PlanStatusDraft || s == PlanStatusPending || s == PlanStatusApproved || s == PlanStatusExecuted
}

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleManager  Role = "manager"
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
)

func (r Role) Valid() bool {
	return r == RoleAdmin || r == RoleManager || r == RoleOperator || r == RoleViewer
}

type RiskLevel string

const (
	RiskNormal   RiskLevel = "normal"
	RiskWarning  RiskLevel = "warning"
	RiskCritical RiskLevel = "critical"
)

type ExecutionStatus string

const (
	ExecutionScheduled ExecutionStatus = "scheduled"
	ExecutionRunning   ExecutionStatus = "running"
	ExecutionCompleted ExecutionStatus = "completed"
	ExecutionCancelled ExecutionStatus = "cancelled"
)

// RestrictionStatus 是停喂安全闸门的生命周期状态。
//
//	active   严重水质异常已自动建立停喂限制，阻止计划批准与投喂执行。
//	handled  操作员已提交处置说明，闸门仍然关闭，等待主管复核。
//	released 主管确认后续读数恢复正常并填写复核依据后解除，恢复正常投喂流程。
type RestrictionStatus string

const (
	RestrictionActive   RestrictionStatus = "active"
	RestrictionHandled  RestrictionStatus = "handled"
	RestrictionReleased RestrictionStatus = "released"
)

func (s RestrictionStatus) Valid() bool {
	return s == RestrictionActive || s == RestrictionHandled || s == RestrictionReleased
}
