package apimodels

type UserPatch struct {
	ID       uint   `json:"id" binding:"required"`
	Callsign string `json:"callsign"`
	Username string `json:"username"`
	Password string `json:"password"`
}
