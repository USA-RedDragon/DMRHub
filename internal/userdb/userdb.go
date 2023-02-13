package userdb

import (
	"bytes"
	"context"
	// Embed the users.json.xz file into the binary.
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ulikunitz/xz"
	"k8s.io/klog/v2"
)

//go:embed userdb-date.txt
var builtInDateStr string

// https://www.radioid.net/static/users.json
//
//go:embed users.json.xz
var compressedDMRUsersDB []byte

var userDB UserDB //nolint:golint,gochecknoglobals

var (
	ErrUpdateFailed = errors.New("update failed")
	ErrUnmarshal    = errors.New("unmarshal failed")
)

const waitTime = 100 * time.Millisecond

type UserDB struct {
	uncompressedJSON       []byte
	dmrUsers               atomic.Value
	dmrUserMap             map[uint]DMRUser
	dmrUserMapLock         sync.RWMutex
	dmrUserMapUpdating     map[uint]DMRUser
	dmrUserMapUpdatingLock sync.RWMutex

	builtInDate time.Time
	isInited    atomic.Bool
	isDone      atomic.Bool
}

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

func IsValidUserID(dmrID uint) bool {
	// Check that the user id is 7 digits
	if dmrID < 1000000 || dmrID > 9999999 {
		return false
	}
	return true
}

func ValidUserCallsign(dmrID uint, callsign string) bool {
	if !userDB.isDone.Load() {
		UnpackDB()
	}
	userDB.dmrUserMapLock.RLock()
	user, ok := userDB.dmrUserMap[dmrID]
	userDB.dmrUserMapLock.RUnlock()
	if !ok {
		return false
	}

	if user.ID != dmrID {
		return false
	}

	if !strings.EqualFold(user.Callsign, callsign) {
		return false
	}

	return true
}

func (e *dmrUserDB) Unmarshal(b []byte) error {
	err := json.Unmarshal(b, e)
	if err != nil {
		return ErrUnmarshal
	}
	return nil
}

func UnpackDB() {
	lastInit := userDB.isInited.Swap(true)
	if !lastInit {
		userDB.dmrUserMapLock.Lock()
		userDB.dmrUserMap = make(map[uint]DMRUser)
		userDB.dmrUserMapLock.Unlock()
		userDB.dmrUserMapUpdatingLock.Lock()
		userDB.dmrUserMapUpdating = make(map[uint]DMRUser)
		userDB.dmrUserMapUpdatingLock.Unlock()
		var err error
		userDB.builtInDate, err = time.Parse(time.RFC3339, builtInDateStr)
		if err != nil {
			klog.Fatalf("Error parsing built-in date: %v", err)
		}
		dbReader, err := xz.NewReader(bytes.NewReader(compressedDMRUsersDB))
		if err != nil {
			klog.Fatalf("NewReader error %s", err)
		}
		userDB.uncompressedJSON, err = io.ReadAll(dbReader)
		if err != nil {
			klog.Fatalf("ReadAll error %s", err)
		}
		var tmpDB dmrUserDB
		if err := json.Unmarshal(userDB.uncompressedJSON, &tmpDB); err != nil {
			klog.Exitf("Error decoding DMR users database: %v", err)
		}
		tmpDB.Date = userDB.builtInDate
		userDB.dmrUsers.Store(tmpDB)
		userDB.dmrUserMapUpdatingLock.Lock()
		for i := range tmpDB.Users {
			userDB.dmrUserMapUpdating[tmpDB.Users[i].ID] = tmpDB.Users[i]
		}
		userDB.dmrUserMapUpdatingLock.Unlock()

		userDB.dmrUserMapLock.Lock()
		userDB.dmrUserMapUpdatingLock.RLock()
		userDB.dmrUserMap = userDB.dmrUserMapUpdating
		userDB.dmrUserMapUpdatingLock.RUnlock()
		userDB.dmrUserMapLock.Unlock()
		userDB.isDone.Store(true)
	}

	for !userDB.isDone.Load() {
		time.Sleep(waitTime)
	}

	usrdb, ok := userDB.dmrUsers.Load().(dmrUserDB)
	if !ok {
		klog.Exit("Error loading DMR users database")
	}
	if len(usrdb.Users) == 0 {
		klog.Exit("No DMR users found in database")
	}
}

func Len() int {
	if !userDB.isDone.Load() {
		UnpackDB()
	}
	db, ok := userDB.dmrUsers.Load().(dmrUserDB)
	if !ok {
		klog.Error("Error loading DMR users database")
	}
	return len(db.Users)
}

func Get(dmrID uint) (DMRUser, bool) {
	if !userDB.isDone.Load() {
		UnpackDB()
	}
	userDB.dmrUserMapLock.RLock()
	user, ok := userDB.dmrUserMap[dmrID]
	userDB.dmrUserMapLock.RUnlock()
	return user, ok
}

func Update() error {
	if !userDB.isDone.Load() {
		UnpackDB()
	}
	const updateTimeout = 10 * time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.radioid.net/static/users.json", nil)
	if err != nil {
		return ErrUpdateFailed
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ErrUpdateFailed
	}

	if resp.StatusCode != http.StatusOK {
		return ErrUpdateFailed
	}

	userDB.uncompressedJSON, err = io.ReadAll(resp.Body)
	if err != nil {
		klog.Errorf("ReadAll error %s", err)
		return ErrUpdateFailed
	}
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			klog.Errorf("Error closing response body: %v", err)
		}
	}()
	var tmpDB dmrUserDB
	if err := json.Unmarshal(userDB.uncompressedJSON, &tmpDB); err != nil {
		klog.Errorf("Error decoding DMR users database: %v", err)
		return ErrUpdateFailed
	}

	if len(tmpDB.Users) == 0 {
		klog.Exit("No DMR users found in database")
	}

	tmpDB.Date = time.Now()
	userDB.dmrUsers.Store(tmpDB)

	userDB.dmrUserMapUpdatingLock.Lock()
	userDB.dmrUserMapUpdating = make(map[uint]DMRUser)
	for i := range tmpDB.Users {
		userDB.dmrUserMapUpdating[tmpDB.Users[i].ID] = tmpDB.Users[i]
	}
	userDB.dmrUserMapUpdatingLock.Unlock()

	userDB.dmrUserMapLock.Lock()
	userDB.dmrUserMapUpdatingLock.RLock()
	userDB.dmrUserMap = userDB.dmrUserMapUpdating
	userDB.dmrUserMapUpdatingLock.RUnlock()
	userDB.dmrUserMapLock.Unlock()

	klog.Infof("Update complete. Loaded %d DMR users", Len())

	return nil
}

func GetDate() time.Time {
	if !userDB.isDone.Load() {
		UnpackDB()
	}
	db, ok := userDB.dmrUsers.Load().(dmrUserDB)
	if !ok {
		klog.Error("Error loading DMR users database")
	}
	return db.Date
}
