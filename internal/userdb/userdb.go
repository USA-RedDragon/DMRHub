package userdb

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ulikunitz/xz"
	"k8s.io/klog/v2"
)

// https://www.radioid.net/static/users.json
//
//go:embed users.json.xz
var compressedDMRUsersDB []byte

var uncompressedJson []byte

type dmrUserDB struct {
	Users []DMRUser `json:"users"`
	Date  time.Time `json:"-"`
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
	user, ok := dmrUserMap[DMRId]
	if !ok {
		return false
	}

	if user.ID != DMRId {
		return false
	}

	if !strings.EqualFold(user.Callsign, callsign) {
		return false
	}

	return true
}

func (e *dmrUserDB) Unmarshal(b []byte) error {
	return json.Unmarshal(b, e)
}

var dmrUsers dmrUserDB

var dmrUserMap map[uint]DMRUser

//go:embed userdb-date.txt
var builtInDateStr string
var builtInDate time.Time

func GetDMRUsers() *map[uint]DMRUser {
	if len(dmrUsers.Users) == 0 {
		var err error
		builtInDate, err = time.Parse(time.RFC3339, builtInDateStr)
		if err != nil {
			klog.Fatalf("Error parsing built-in date: %v", err)
		}
		dmrUsers.Date = builtInDate
		dbReader, err := xz.NewReader(bytes.NewReader(compressedDMRUsersDB))
		if err != nil {
			klog.Fatalf("NewReader error %s", err)
		}
		uncompressedJson, err = io.ReadAll(dbReader)
		if err != nil {
			klog.Fatalf("ReadAll error %s", err)
		}
		if err := json.Unmarshal(uncompressedJson, &dmrUsers); err != nil {
			klog.Exitf("Error decoding DMR users database: %v", err)
		}
		dmrUserMap = make(map[uint]DMRUser)
		for i := range dmrUsers.Users {
			dmrUserMap[dmrUsers.Users[i].ID] = dmrUsers.Users[i]
		}
	}

	if len(dmrUsers.Users) == 0 {
		klog.Exit("No DMR users found in database")
	}
	return &dmrUserMap
}

func Update() error {
	resp, err := http.Get("https://www.radioid.net/static/users.json")
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	uncompressedJson, err = io.ReadAll(resp.Body)
	if err != nil {
		klog.Errorf("ReadAll error %s", err)
		return err
	}
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			klog.Errorf("Error closing response body: %v", err)
		}
	}()
	if err := json.Unmarshal(uncompressedJson, &dmrUsers); err != nil {
		klog.Errorf("Error decoding DMR users database: %v", err)
		return err
	}

	if len(dmrUsers.Users) == 0 {
		klog.Exit("No DMR users found in database")
	}

	klog.Infof("Update complete. Loaded %d DMR users", len(dmrUsers.Users))

	return nil
}

func GetDate() time.Time {
	return dmrUsers.Date
}
