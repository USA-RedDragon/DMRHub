package userdb

import (
	"strings"
	"testing"
)

func TestUserdb(t *testing.T) {
	t.Parallel()
	if Len() == 0 {
		t.Error("dmrUsers is empty")
	}
	// Check for an obviously wrong number of IDs.
	// As of writing this test, there are 232,772 IDs in the database
	if Len() < 231290 {
		t.Errorf("dmrUsers is missing users, found %d users", Len())
	}
}

func TestUserdbValidUser(t *testing.T) {
	t.Parallel()
	if !ValidUserCallsign(3191868, "KI5VMF") {
		t.Error("KI5VMF is not in the database")
	}
	me, ok := Get(3191868)
	if !ok {
		t.Error("KI5VMF is not in the database")
	}
	if me.ID != 3191868 {
		t.Errorf("KI5VMF has the wrong ID. Expected %d, got %d", 3191868, me.ID)
	}
	if !strings.EqualFold(me.Callsign, "KI5VMF") {
		t.Errorf("KI5VMF has the wrong callsign. Expected \"%s\", got \"%s\"", "KI5VMF", me.Callsign)
	}
}

func TestUserdbInvalidUser(t *testing.T) {
	t.Parallel()
	// DMR User IDs are 7 digits.
	if IsValidUserID(10000000) {
		t.Error("10000000 is not a valid user ID")
	}
	// 6 digits
	if IsValidUserID(999999) {
		t.Error("999999 is not a valid user ID")
	}
	// 5 digits
	if IsValidUserID(99999) {
		t.Error("99999 is not a valid user ID")
	}
	// 4 digits
	if IsValidUserID(9999) {
		t.Error("9999 is not a valid user ID")
	}
	// 3 digits
	if IsValidUserID(999) {
		t.Error("999 is not a valid user ID")
	}
	// 2 digits
	if IsValidUserID(99) {
		t.Error("99 is not a valid user ID")
	}
	// 1 digit
	if IsValidUserID(9) {
		t.Error("9 is not a valid user ID")
	}
	// 0 digits
	if IsValidUserID(0) {
		t.Error("0 is not a valid user ID")
	}
	if !IsValidUserID(3191868) {
		t.Error("Valid user ID marked invalid")
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

func BenchmarkUserDB(b *testing.B) {
	for i := 0; i < b.N; i++ {
		UnpackDB()
		dmrUsers = dmrUserDB{}
	}
}

func BenchmarkUserSearch(b *testing.B) {
	// The first run will decompress the database, so we'll do that first
	for i := 0; i < b.N; i++ {
		ValidUserCallsign(3191868, "KI5VMF")
	}
}
