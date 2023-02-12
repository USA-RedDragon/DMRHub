package repeaterdb

import (
	"bytes"
	"context"
	// Embed the repeaters.json.xz file into the binary.
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ulikunitz/xz"
	"k8s.io/klog/v2"
)

// https://www.radioid.net/static/rptrs.json
//
//go:embed repeaters.json.xz
var comressedDMRRepeatersDB []byte

var uncompressedJSON []byte

type dmrRepeaterDB struct {
	Repeaters []DMRRepeater `json:"rptrs"`
	Date      time.Time     `json:"-"`
}

type DMRRepeater struct {
	Locator     string `json:"locator"`
	ID          string `json:"id"`
	Callsign    string `json:"callsign"`
	City        string `json:"city"`
	State       string `json:"state"`
	Country     string `json:"country"`
	Frequency   string `json:"frequency"`
	ColorCode   uint   `json:"color_code"`
	Offset      string `json:"offset"`
	Assigned    string `json:"assigned"`
	TSLinked    string `json:"ts_linked"`
	Trustee     string `json:"trustee"`
	MapInfo     string `json:"map_info"`
	Map         uint   `json:"map"`
	IPSCNetwork string `json:"ipsc_network"`
}

func IsValidRepeaterID(dmrID uint) bool {
	// Check that the repeater id is 6 digits
	if dmrID < 100000 || dmrID > 999999 {
		return false
	}
	return true
}

func ValidRepeaterCallsign(dmrID uint, callsign string) bool {
	if !isDone.Load() {
		UnpackDB()
	}
	dmrRepeaterMapLock.RLock()
	repeater, ok := dmrRepeaterMap[dmrID]
	dmrRepeaterMapLock.RUnlock()
	if !ok {
		return false
	}

	if !strings.EqualFold(repeater.Trustee, callsign) {
		return false
	}

	return true
}

func (e *dmrRepeaterDB) Unmarshal(b []byte) error {
	return json.Unmarshal(b, e)
}

var dmrRepeaters atomic.Value

var dmrRepeaterMap map[uint]DMRRepeater
var dmrRepeaterMapLock sync.RWMutex

// Used to update the user map atomically.
var dmrRepeaterMapUpdating map[uint]DMRRepeater
var dmrRepeaterMapUpdatingLock sync.RWMutex

//go:embed repeaterdb-date.txt
var builtInDateStr string
var builtInDate time.Time

var isInited atomic.Bool
var isDone atomic.Bool

func UnpackDB() {
	lastInit := isInited.Swap(true)
	if !lastInit {
		dmrRepeaterMapLock.Lock()
		dmrRepeaterMap = make(map[uint]DMRRepeater)
		dmrRepeaterMapLock.Unlock()
		dmrRepeaterMapUpdatingLock.Lock()
		dmrRepeaterMapUpdating = make(map[uint]DMRRepeater)
		dmrRepeaterMapUpdatingLock.Unlock()
		var err error
		builtInDate, err = time.Parse(time.RFC3339, builtInDateStr)
		if err != nil {
			klog.Fatalf("Error parsing built-in date: %v", err)
		}
		dbReader, err := xz.NewReader(bytes.NewReader(comressedDMRRepeatersDB))
		if err != nil {
			klog.Fatalf("NewReader error %s", err)
		}
		uncompressedJSON, err = io.ReadAll(dbReader)
		if err != nil {
			klog.Fatalf("ReadAll error %s", err)
		}
		var tmpDB dmrRepeaterDB
		if err := json.Unmarshal(uncompressedJSON, &tmpDB); err != nil {
			klog.Exitf("Error decoding DMR repeaters database: %v", err)
		}

		tmpDB.Date = builtInDate
		dmrRepeaters.Store(tmpDB)
		dmrRepeaterMapUpdatingLock.Lock()
		for i := range tmpDB.Repeaters {
			id, err := strconv.Atoi(tmpDB.Repeaters[i].ID)
			if err != nil {
				klog.Errorf("Error converting repeater ID to int: %v", err)
				continue
			}
			dmrRepeaterMapUpdating[uint(id)] = tmpDB.Repeaters[i]
		}
		dmrRepeaterMapUpdatingLock.Unlock()

		dmrRepeaterMapLock.Lock()
		dmrRepeaterMapUpdatingLock.RLock()
		dmrRepeaterMap = dmrRepeaterMapUpdating
		dmrRepeaterMapUpdatingLock.RUnlock()
		dmrRepeaterMapLock.Unlock()
		isDone.Store(true)
	}

	for !isDone.Load() {
		time.Sleep(100 * time.Millisecond)
	}

	rptdb, ok := dmrRepeaters.Load().(dmrRepeaterDB)
	if !ok {
		klog.Exit("Error loading DMR users database")
	}
	if len(rptdb.Repeaters) == 0 {
		klog.Exit("No DMR users found in database")
	}
}

func Len() int {
	if !isDone.Load() {
		UnpackDB()
	}
	db, ok := dmrRepeaters.Load().(dmrRepeaterDB)
	if !ok {
		klog.Error("Error loading DMR users database")
	}
	return len(db.Repeaters)
}

func Get(id uint) (DMRRepeater, bool) {
	if !isDone.Load() {
		UnpackDB()
	}
	dmrRepeaterMapLock.RLock()
	user, ok := dmrRepeaterMap[id]
	dmrRepeaterMapLock.RUnlock()
	return user, ok
}

func Update() error {
	if !isDone.Load() {
		UnpackDB()
	}
	const updateTimeout = 10 * time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.radioid.net/static/rptrs.json", nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
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
	var tmpDB dmrRepeaterDB
	if err := json.Unmarshal(uncompressedJSON, &tmpDB); err != nil {
		klog.Errorf("Error decoding DMR repeaters database: %v", err)
		return err
	}

	if len(tmpDB.Repeaters) == 0 {
		klog.Exit("No DMR repeaters found in database")
	}

	tmpDB.Date = time.Now()
	dmrRepeaters.Store(tmpDB)

	dmrRepeaterMapUpdatingLock.Lock()
	dmrRepeaterMapUpdating = make(map[uint]DMRRepeater)
	for i := range tmpDB.Repeaters {
		id, err := strconv.Atoi(tmpDB.Repeaters[i].ID)
		if err != nil {
			klog.Errorf("Error converting repeater ID to int: %v", err)
			continue
		}
		dmrRepeaterMapUpdating[uint(id)] = tmpDB.Repeaters[i]
	}
	dmrRepeaterMapUpdatingLock.Unlock()

	dmrRepeaterMapLock.Lock()
	dmrRepeaterMapUpdatingLock.RLock()
	dmrRepeaterMap = dmrRepeaterMapUpdating
	dmrRepeaterMapUpdatingLock.RUnlock()
	dmrRepeaterMapLock.Unlock()

	klog.Infof("Update complete. Loaded %d DMR repeaters", Len())

	return nil
}

func GetDate() time.Time {
	if !isDone.Load() {
		UnpackDB()
	}
	db, ok := dmrRepeaters.Load().(dmrRepeaterDB)
	if !ok {
		klog.Error("Error loading DMR users database")
	}
	return db.Date
}
