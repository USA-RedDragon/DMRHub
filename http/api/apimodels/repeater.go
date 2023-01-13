package apimodels

type RepeaterPost struct {
	RadioID    uint   `json:"radio_id" binding:"required"`
	Password   string `json:"password" binding:"required"`
	SecureMode bool   `json:"secure_mode"`
}

type RepeaterPatch struct {
	Password   string `json:"password"`
	SecureMode bool   `json:"secure_mode"`
}
