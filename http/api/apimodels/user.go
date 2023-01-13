package apimodels

import "regexp"

type UserRegistration struct {
	DMRId    uint   `json:"dmr_id" binding:"required"`
	Callsign string `json:"callsign" binding:"required"`
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

var isValidUsernameCharset = regexp.MustCompile(`^[a-zA-Z0-9_\-\.]+$`).MatchString

func (r *UserRegistration) IsValidUsername() (bool, string) {
	if len(r.Username) < 3 {
		return false, "Username must be at least 3 characters"
	}
	if len(r.Username) > 20 {
		return false, "Username must be less than 20 characters"
	}
	if !isValidUsernameCharset(r.Username) {
		return false, "Username must be alphanumeric, _, -, or ."
	}
	return true, ""
}

type UserPatch struct {
	Callsign string `json:"callsign"`
	Username string `json:"username"`
	Password string `json:"password"`
}
