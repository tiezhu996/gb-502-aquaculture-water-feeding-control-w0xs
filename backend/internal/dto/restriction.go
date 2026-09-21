package dto

type DisposeRestrictionInput struct {
	DispositionNote string `json:"dispositionNote" binding:"required,min=5,max=1000"`
}

type ReleaseRestrictionInput struct {
	ReleaseBasis string `json:"releaseBasis" binding:"required,min=5,max=1000"`
}
