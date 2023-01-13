package apimodels

type UserAdmins struct {
	ID    uint `json:"id" binding:"required"`
	Admin bool `json:"admin" binding:"required"`
}
