package repeaterdb

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/ulikunitz/xz"
	"k8s.io/klog/v2"
)

// https://www.radioid.net/static/rptrs.json
//
//go:embed repeaters.json.xz
var comressedDMRRepeatersDB []byte

var uncompressedDB []byte
var uncompressedJson []byte

type dmrRepeaterDB struct {
	Repeaters []DMRRepeater `json:"rptrs"`
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
	matchesTrustee := false
	registeredDMRID := false

	for _, repeater := range *GetDMRRepeaters() {
		if repeater.ID == fmt.Sprintf("%d", DMRId) {
			registeredDMRID = true
		}

		if strings.EqualFold(repeater.Trustee, callsign) {
			matchesTrustee = true
			if registeredDMRID {
				break
			}
		}
		registeredDMRID = false
		matchesTrustee = false
	}
	if registeredDMRID && matchesTrustee {
		return true
	}
	return false
}

func (e *dmrRepeaterDB) Unmarshal(b []byte) error {
	return json.Unmarshal(b, e)
}

var dmrRepeaters dmrRepeaterDB

func GetDMRRepeaters() *[]DMRRepeater {
	if len(dmrRepeaters.Repeaters) == 0 {
		uncompressedDB, err := xz.NewReader(bytes.NewReader(comressedDMRRepeatersDB))
		if err != nil {
			klog.Fatalf("NewReader error %s", err)
		}
		uncompressedJson, err = io.ReadAll(uncompressedDB)
		if err != nil {
			klog.Fatalf("ReadAll error %s", err)
		}
		if err := json.Unmarshal(uncompressedJson, &dmrRepeaters); err != nil {
			klog.Exitf("Error decoding DMR repeaters database: %v", err)
		}
	}

	if len(dmrRepeaters.Repeaters) == 0 {
		klog.Exit("No DMR repeaters found in database")
	}
	return &dmrRepeaters.Repeaters
}
