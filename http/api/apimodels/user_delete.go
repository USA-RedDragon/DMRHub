package apimodels

type UserDelete struct {
	ID uint `json:"id" binding:"required"`
}
