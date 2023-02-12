package apimodels

import "github.com/USA-RedDragon/DMRHub/internal/db/models"

type RepeaterPost struct {
	RadioID uint `json:"id" binding:"required"`
}

type RepeaterTalkgroupsPost struct {
	TS1StaticTalkgroups []models.Talkgroup `json:"ts1_static_talkgroups"`
	TS2StaticTalkgroups []models.Talkgroup `json:"ts2_static_talkgroups"`
	TS1DynamicTalkgroup models.Talkgroup   `json:"ts1_dynamic_talkgroup"`
	TS2DynamicTalkgroup models.Talkgroup   `json:"ts2_dynamic_talkgroup"`
}
