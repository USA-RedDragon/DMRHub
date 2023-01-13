package userdb

import (
	"bufio"
	"bytes"
	_ "embed"
	"encoding/json"

	"k8s.io/klog/v2"
)

//go:embed users.json
var dmrUsersDB []byte

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

func (e *dmrUserDB) Unmarshal(b []byte) error {
	return json.Unmarshal(b, e)
}

var dmrUsers dmrUserDB

func GetDMRUsers() *[]DMRUser {
	if len(dmrUsers.Users) == 0 {
		dbFile := bytes.NewReader(dmrUsersDB)

		r := bufio.NewReader(dbFile)
		d := json.NewDecoder(r)

		if err := d.Decode(&dmrUsers); err != nil {
			klog.Exitf("Error decoding DMR users database: %v", err)
		}
	}

	if len(dmrUsers.Users) == 0 {
		klog.Exit("No DMR users found in database")
	}
	return &dmrUsers.Users
}
