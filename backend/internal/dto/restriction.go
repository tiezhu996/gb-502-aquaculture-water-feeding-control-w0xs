package dto

// HandleRestrictionInput 操作员对停喂限制提交现场处置说明。
type HandleRestrictionInput struct {
	Note string `json:"note" binding:"required,min=2,max=500"`
}

// ReleaseRestrictionInput 主管解除停喂限制时填写复核依据。
type ReleaseRestrictionInput struct {
	ReviewNote string `json:"reviewNote" binding:"required,min=2,max=500"`
}
