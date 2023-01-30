package apimodels

type AuthLogin struct {
	Username string `json:"username"`
	Callsign string `json:"callsign"`
	Password string `json:"password" binding:"required"`
}
