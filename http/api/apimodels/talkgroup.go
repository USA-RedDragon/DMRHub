package apimodels

type TalkgroupPost struct {
	ID          uint   `json:"id" binding:"required"`
	Name        string `json:"name" binding:"required"`
	Description string `json:"description" binding:"required"`
}

type TalkgroupPatch struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type TalkgroupAdminAction struct {
	UserID uint `json:"user_id"`
}
