package userdb

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"io"
	"strings"

	"github.com/ulikunitz/xz"
	"k8s.io/klog/v2"
)

// https://www.radioid.net/static/users.json
//
//go:embed users.json.xz
var comressedDMRUsersDB []byte

var uncompressedDB []byte
var uncompressedJson []byte

type dmrUserDB struct {
	Users []DMRUser `json:"users"`
}

type DMRUser struct {
	ID       uint   `json:"id"`
	State    string `json:"state"`
	RadioID  uint   `json:"radio_id"`
	Surname  string `json:"surname"`
	City     string `json:"city"`
	Callsign string `json:"callsign"`
	Country  string `json:"country"`
	Name     string `json:"name"`
	FName    string `json:"fname"`
}

func IsValidUserID(DMRId uint) bool {
	// Check that the user id is 7 digits
	if DMRId < 1000000 || DMRId > 9999999 {
		return false
	}
	return true
}

func IsInDB(DMRId uint, callsign string) bool {
	matchesCallsign := false
	registeredDMRID := false

	for _, user := range *GetDMRUsers() {
		if user.ID == DMRId {
			registeredDMRID = true
		}

		if strings.EqualFold(user.Callsign, callsign) {
			matchesCallsign = true
			if registeredDMRID {
				break
			}
		}
		registeredDMRID = false
		matchesCallsign = false
	}
	if registeredDMRID && matchesCallsign {
		return true
	}
	return false
}

func (e *dmrUserDB) Unmarshal(b []byte) error {
	return json.Unmarshal(b, e)
}

var dmrUsers dmrUserDB

func GetDMRUsers() *[]DMRUser {
	if len(dmrUsers.Users) == 0 {
		uncompressedDB, err := xz.NewReader(bytes.NewReader(comressedDMRUsersDB))
		if err != nil {
			klog.Fatalf("NewReader error %s", err)
		}
		uncompressedJson, err = io.ReadAll(uncompressedDB)
		if err != nil {
			klog.Fatalf("ReadAll error %s", err)
		}
		if err := json.Unmarshal(uncompressedJson, &dmrUsers); err != nil {
			klog.Exitf("Error decoding DMR users database: %v", err)
		}
	}

	if len(dmrUsers.Users) == 0 {
		klog.Exit("No DMR users found in database")
	}
	return &dmrUsers.Users
}
