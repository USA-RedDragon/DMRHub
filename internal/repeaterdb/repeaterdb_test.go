package repeaterdb

import (
	"strings"
	"testing"
)

func TestRepeaterdb(t *testing.T) {
	t.Parallel()
	dmrRepeaters := GetDMRRepeaters()
	if len(*dmrRepeaters) == 0 {
		t.Error("dmrRepeaters is empty")
	}
	// Check for an obviously wrong number of IDs.
	// As of writing this test, there are 9200 IDs in the database
	if len(*dmrRepeaters) < 9200 {
		t.Errorf("dmrRepeaters is missing repeaters, found %d repeaters", len(*dmrRepeaters))
	}
}

func TestRepeaterdbValidRepeater(t *testing.T) {
	t.Parallel()
	dmrRepeaters := GetDMRRepeaters()
	if !IsInDB(313060, "KP4DJT") {
		t.Error("KP4DJT is not in the database")
	}
	repeater, ok := (*dmrRepeaters)[313060]
	if !ok {
		t.Error("KP4DJT is not in the database")
	}
	if !strings.EqualFold(repeater.ID, "313060") {
		t.Errorf("KP4DJT has the wrong ID. Expected %d, got %s", 313060, repeater.ID)
	}
	if !strings.EqualFold(repeater.Callsign, "KP4DJT") {
		t.Errorf("KP4DJT has the wrong callsign. Expected \"%s\", got \"%s\"", "KP4DJT", repeater.Callsign)
	}
}

func TestRepeaterdbInvalidRepeater(t *testing.T) {
	t.Parallel()
	// DMR repeater IDs are 6 digits.
	// 7 digits
	if IsValidRepeaterID(9999999) {
		t.Error("9999999 is not a valid repeater ID")
	}
	// 5 digits
	if IsValidRepeaterID(99999) {
		t.Error("99999 is not a valid repeater ID")
	}
	// 4 digits
	if IsValidRepeaterID(9999) {
		t.Error("9999 is not a valid repeater ID")
	}
	// 3 digits
	if IsValidRepeaterID(999) {
		t.Error("999 is not a valid repeater ID")
	}
	// 2 digits
	if IsValidRepeaterID(99) {
		t.Error("99 is not a valid repeater ID")
	}
	// 1 digit
	if IsValidRepeaterID(9) {
		t.Error("9 is not a valid repeater ID")
	}
	// 0 digits
	if IsValidRepeaterID(0) {
		t.Error("0 is not a valid repeater ID")
	}
	if !IsValidRepeaterID(313060) {
		t.Error("Valid repeater ID marked invalid")
	}
}

func TestUpdate(t *testing.T) {
	t.Parallel()
	err := Update()
	if err != nil {
		t.Error(err)
	}
	if builtInDate == GetDate() {
		t.Error("Update did not update the database")
	}
}

func BenchmarkRepeaterDB(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GetDMRRepeaters()
		dmrRepeaters = dmrRepeaterDB{}
	}
}

func BenchmarkRepeaterSearch(b *testing.B) {
	// The first run will decompress the database, so we'll do that first
	b.StopTimer()
	GetDMRRepeaters()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		IsInDB(313060, "KP4DJT")
	}
}
