package repeaterdb

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"io"

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
	Locator     uint    `json:"locator"`
	ID          uint    `json:"id"`
	Callsign    string  `json:"callsign"`
	City        string  `json:"city"`
	State       string  `json:"state"`
	Country     string  `json:"country"`
	Frequency   float32 `json:"frequency"`
	ColorCode   uint    `json:"color_code"`
	Offset      float32 `json:"offset"`
	Assigned    string  `json:"assigned"`
	TSLinked    string  `json:"ts_linked"`
	Trustee     string  `json:"trustee"`
	MapInfo     string  `json:"map_info"`
	Map         uint    `json:"map"`
	IPSCNetwork string  `json:"ipsc_network"`
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
