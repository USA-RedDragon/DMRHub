package repeaterdb

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ulikunitz/xz"
	"k8s.io/klog/v2"
)

// https://www.radioid.net/static/rptrs.json
//
//go:embed repeaters.json.xz
var comressedDMRRepeatersDB []byte

var uncompressedJson []byte

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

func IsValidRepeaterID(DMRId uint) bool {
	// Check that the repeater id is 6 digits
	if DMRId < 100000 || DMRId > 999999 {
		return false
	}
	return true
}

func IsInDB(DMRId uint, callsign string) bool {
	repeater, ok := dmrRepeaterMap[DMRId]
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

var dmrRepeaters dmrRepeaterDB

var dmrRepeaterMap map[uint]DMRRepeater

//go:embed repeaterdb-date.txt
var builtInDateStr string
var builtInDate time.Time

func GetDMRRepeaters() *map[uint]DMRRepeater {
	if len(dmrRepeaters.Repeaters) == 0 {
		var err error
		builtInDate, err = time.Parse(time.RFC3339, builtInDateStr)
		if err != nil {
			klog.Fatalf("Error parsing built-in date: %v", err)
		}
		dmrRepeaters.Date = builtInDate
		dbReader, err := xz.NewReader(bytes.NewReader(comressedDMRRepeatersDB))
		if err != nil {
			klog.Fatalf("NewReader error %s", err)
		}
		uncompressedJson, err = io.ReadAll(dbReader)
		if err != nil {
			klog.Fatalf("ReadAll error %s", err)
		}
		if err := json.Unmarshal(uncompressedJson, &dmrRepeaters); err != nil {
			klog.Exitf("Error decoding DMR repeaters database: %v", err)
		}

		dmrRepeaterMap = make(map[uint]DMRRepeater)
		for i := range dmrRepeaters.Repeaters {
			id, err := strconv.Atoi(dmrRepeaters.Repeaters[i].ID)
			if err != nil {
				klog.Errorf("Error converting repeater ID to int: %v", err)
				continue
			}
			dmrRepeaterMap[uint(id)] = dmrRepeaters.Repeaters[i]
		}
	}

	if len(dmrRepeaters.Repeaters) == 0 {
		klog.Exit("No DMR repeaters found in database")
	}
	return &dmrRepeaterMap
}

func GetRepeater(id uint) (DMRRepeater, error) {
	for _, repeater := range *GetDMRRepeaters() {
		if repeater.ID == fmt.Sprintf("%d", id) {
			return repeater, nil
		}
	}
	return DMRRepeater{}, errors.New("repeater not found")
}

func Update() error {
	resp, err := http.Get("https://www.radioid.net/static/rptrs.json")
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
	if err := json.Unmarshal(uncompressedJson, &dmrRepeaters); err != nil {
		klog.Errorf("Error decoding DMR repeaters database: %v", err)
		return err
	}

	if len(dmrRepeaters.Repeaters) == 0 {
		klog.Exit("No DMR repeaters found in database")
	}

	klog.Infof("Update complete. Loaded %d DMR repeaters", len(dmrRepeaters.Repeaters))

	return nil
}

func GetDate() time.Time {
	return dmrRepeaters.Date
}
