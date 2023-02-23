package apimodels

import "time"

type WSCallResponseUser struct {
	ID       uint   `json:"id"`
	Callsign string `json:"callsign"`
}

type WSCallResponseRepeater struct {
	RadioID  uint   `json:"radio_id"`
	Callsign string `json:"callsign"`
}

type WSCallResponseTalkgroup struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type WSCallResponse struct {
	ID            uint                    `json:"id"`
	User          WSCallResponseUser      `json:"user"`
	StartTime     time.Time               `json:"start_time"`
	Duration      time.Duration           `json:"duration"`
	Active        bool                    `json:"active"`
	TimeSlot      bool                    `json:"time_slot"`
	GroupCall     bool                    `json:"group_call"`
	IsToTalkgroup bool                    `json:"is_to_talkgroup"`
	ToTalkgroup   WSCallResponseTalkgroup `json:"to_talkgroup"`
	IsToUser      bool                    `json:"is_to_user"`
	ToUser        WSCallResponseUser      `json:"to_user"`
	IsToRepeater  bool                    `json:"is_to_repeater"`
	ToRepeater    WSCallResponseRepeater  `json:"to_repeater"`
	Loss          float32                 `json:"loss"`
	Jitter        float32                 `json:"jitter"`
	BER           float32                 `json:"ber"`
	RSSI          float32                 `json:"rssi"`
}
