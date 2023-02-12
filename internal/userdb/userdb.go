package userdb

import (
	"bytes"
	"sync"

	// Embed the users.json.xz file into the binary
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

var uncompressedJSON []byte

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

func ValidUserCallsign(DMRId uint, callsign string) bool {
	if !isDone {
		UnpackDB()
	}
	dmrUserMapLock.RLock()
	user, ok := dmrUserMap[DMRId]
	dmrUserMapLock.RUnlock()
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
var dmrUserMapLock sync.RWMutex

// Used to update the user map atomically
var dmrUserMapUpdating map[uint]DMRUser
var dmrUserMapUpdatingLock sync.RWMutex

//go:embed userdb-date.txt
var builtInDateStr string
var builtInDate time.Time

var isInited bool
var isDone bool

func UnpackDB() {
	if len(dmrUsers.Users) == 0 && !isInited {
		isInited = true
		dmrUserMapLock.Lock()
		dmrUserMap = make(map[uint]DMRUser)
		dmrUserMapLock.Unlock()
		dmrUserMapUpdatingLock.Lock()
		dmrUserMapUpdating = make(map[uint]DMRUser)
		dmrUserMapUpdatingLock.Unlock()
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
		uncompressedJSON, err = io.ReadAll(dbReader)
		if err != nil {
			klog.Fatalf("ReadAll error %s", err)
		}
		if err := json.Unmarshal(uncompressedJSON, &dmrUsers); err != nil {
			klog.Exitf("Error decoding DMR users database: %v", err)
		}
		dmrUserMapUpdatingLock.Lock()
		for i := range dmrUsers.Users {
			dmrUserMapUpdating[dmrUsers.Users[i].ID] = dmrUsers.Users[i]
		}
		dmrUserMapUpdatingLock.Unlock()

		dmrUserMapLock.Lock()
		dmrUserMapUpdatingLock.RLock()
		dmrUserMap = dmrUserMapUpdating
		dmrUserMapUpdatingLock.RUnlock()
		dmrUserMapLock.Unlock()
		isDone = true
	}

	for !isDone {
		time.Sleep(100 * time.Millisecond)
	}

	if len(dmrUsers.Users) == 0 {
		klog.Exit("No DMR users found in database")
	}
}

func Len() int {
	if !isDone {
		UnpackDB()
	}
	return len(dmrUsers.Users)
}

func Get(DMRId uint) (DMRUser, bool) {
	if !isDone {
		UnpackDB()
	}
	dmrUserMapLock.RLock()
	user, ok := dmrUserMap[DMRId]
	dmrUserMapLock.RUnlock()
	return user, ok
}

func Update() error {
	if !isDone {
		UnpackDB()
	}
	resp, err := http.Get("https://www.radioid.net/static/users.json")
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	uncompressedJSON, err = io.ReadAll(resp.Body)
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
	if err := json.Unmarshal(uncompressedJSON, &dmrUsers); err != nil {
		klog.Errorf("Error decoding DMR users database: %v", err)
		return err
	}

	if len(dmrUsers.Users) == 0 {
		klog.Exit("No DMR users found in database")
	}

	dmrUserMapUpdatingLock.Lock()
	dmrUserMapUpdating = make(map[uint]DMRUser)
	for i := range dmrUsers.Users {
		dmrUserMapUpdating[dmrUsers.Users[i].ID] = dmrUsers.Users[i]
	}
	dmrUserMapUpdatingLock.Unlock()

	dmrUserMapLock.Lock()
	dmrUserMapUpdatingLock.RLock()
	dmrUserMap = dmrUserMapUpdating
	dmrUserMapUpdatingLock.RUnlock()
	dmrUserMapLock.Unlock()

	dmrUsers.Date = time.Now()

	klog.Infof("Update complete. Loaded %d DMR users", len(dmrUsers.Users))

	return nil
}

func GetDate() time.Time {
	if !isDone {
		UnpackDB()
	}
	return dmrUsers.Date
}
