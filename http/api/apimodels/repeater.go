package apimodels

type RepeaterPost struct {
	RadioID  uint   `json:"id" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type RepeaterPatch struct {
	Password string `json:"password"`
}
