package apimodels

import "regexp"

const minUsernameLength = 3
const maxUsernameLength = 20

type UserRegistration struct {
	DMRId    uint   `json:"id" binding:"required"`
	Callsign string `json:"callsign" binding:"required"`
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func (r *UserRegistration) IsValidUsername() (bool, string) {
	if len(r.Username) < minUsernameLength {
		return false, "Username must be at least 3 characters"
	}
	if len(r.Username) > maxUsernameLength {
		return false, "Username must be less than 20 characters"
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9_\-\.]+$`).MatchString(r.Username) {
		return false, "Username must be alphanumeric, _, -, or ."
	}
	return true, ""
}

type UserPatch struct {
	Callsign string `json:"callsign"`
	Username string `json:"username"`
	Password string `json:"password"`
}
