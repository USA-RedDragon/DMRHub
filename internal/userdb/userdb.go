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

	"sync/atomic"

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
	if !isDone.Load() {
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

var dmrUsers atomic.Value

var dmrUserMap map[uint]DMRUser
var dmrUserMapLock sync.RWMutex

// Used to update the user map atomically
var dmrUserMapUpdating map[uint]DMRUser
var dmrUserMapUpdatingLock sync.RWMutex

//go:embed userdb-date.txt
var builtInDateStr string
var builtInDate time.Time

var isInited atomic.Bool
var isDone atomic.Bool

func UnpackDB() {
	lastInit := isInited.Swap(true)
	if !lastInit {
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
		dbReader, err := xz.NewReader(bytes.NewReader(compressedDMRUsersDB))
		if err != nil {
			klog.Fatalf("NewReader error %s", err)
		}
		uncompressedJSON, err = io.ReadAll(dbReader)
		if err != nil {
			klog.Fatalf("ReadAll error %s", err)
		}
		var tmpDB dmrUserDB
		if err := json.Unmarshal(uncompressedJSON, &tmpDB); err != nil {
			klog.Exitf("Error decoding DMR users database: %v", err)
		}
		tmpDB.Date = builtInDate
		dmrUsers.Store(tmpDB)
		dmrUserMapUpdatingLock.Lock()
		for i := range tmpDB.Users {
			dmrUserMapUpdating[tmpDB.Users[i].ID] = tmpDB.Users[i]
		}
		dmrUserMapUpdatingLock.Unlock()

		dmrUserMapLock.Lock()
		dmrUserMapUpdatingLock.RLock()
		dmrUserMap = dmrUserMapUpdating
		dmrUserMapUpdatingLock.RUnlock()
		dmrUserMapLock.Unlock()
		isDone.Store(true)
	}

	for !isDone.Load() {
		time.Sleep(100 * time.Millisecond)
	}

	if len(dmrUsers.Load().(dmrUserDB).Users) == 0 {
		klog.Exit("No DMR users found in database")
	}
}

func Len() int {
	if !isDone.Load() {
		UnpackDB()
	}
	return len(dmrUsers.Load().(dmrUserDB).Users)
}

func Get(DMRId uint) (DMRUser, bool) {
	if !isDone.Load() {
		UnpackDB()
	}
	dmrUserMapLock.RLock()
	user, ok := dmrUserMap[DMRId]
	dmrUserMapLock.RUnlock()
	return user, ok
}

func Update() error {
	if !isDone.Load() {
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
	var tmpDB dmrUserDB
	if err := json.Unmarshal(uncompressedJSON, &tmpDB); err != nil {
		klog.Errorf("Error decoding DMR users database: %v", err)
		return err
	}

	if len(tmpDB.Users) == 0 {
		klog.Exit("No DMR users found in database")
	}

	tmpDB.Date = time.Now()
	dmrUsers.Store(tmpDB)

	dmrUserMapUpdatingLock.Lock()
	dmrUserMapUpdating = make(map[uint]DMRUser)
	for i := range tmpDB.Users {
		dmrUserMapUpdating[tmpDB.Users[i].ID] = tmpDB.Users[i]
	}
	dmrUserMapUpdatingLock.Unlock()

	dmrUserMapLock.Lock()
	dmrUserMapUpdatingLock.RLock()
	dmrUserMap = dmrUserMapUpdating
	dmrUserMapUpdatingLock.RUnlock()
	dmrUserMapLock.Unlock()

	klog.Infof("Update complete. Loaded %d DMR users", Len())

	return nil
}

func GetDate() time.Time {
	if !isDone.Load() {
		UnpackDB()
	}
	return dmrUsers.Load().(dmrUserDB).Date
}
