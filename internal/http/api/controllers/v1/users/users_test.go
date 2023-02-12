package users_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/internal/userdb"
	"github.com/stretchr/testify/assert"
)

func TestRegisterBadUser(t *testing.T) {
	router := testutils.CreateRouter()
	defer testutils.CloseRedis()
	defer testutils.CloseDB()

	// Test invalid user
	user := apimodels.UserRegistration{
		DMRId:    0,
		Callsign: "",
		Username: "",
		Password: "",
	}

	jsonBytes, err := json.Marshal(user)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/users", bytes.NewBuffer(jsonBytes))
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
	assert.NotEmpty(t, w.Body.String())

	var resp testutils.APIResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)

	assert.NoError(t, err)
	assert.Empty(t, resp.Message)
	assert.NotEmpty(t, resp.Error)
	assert.Equal(t, resp.Error, "JSON data is invalid")
}

func TestRegisterBadData(t *testing.T) {
	router := testutils.CreateRouter()
	defer testutils.CloseRedis()
	defer testutils.CloseDB()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/users", bytes.NewBuffer([]byte("invalid json data")))
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
	assert.NotEmpty(t, w.Body.String())

	var resp testutils.APIResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)

	assert.NoError(t, err)
	assert.Empty(t, resp.Message)
	assert.NotEmpty(t, resp.Error)
	assert.Equal(t, resp.Error, "JSON data is invalid")
}

func TestRegisterBadDMRId(t *testing.T) {
	router := testutils.CreateRouter()
	defer testutils.CloseRedis()
	defer testutils.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    1,
		Callsign: "N0CALL",
		Username: "n0call",
		Password: "password",
	}

	jsonBytes, err := json.Marshal(user)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/users", bytes.NewBuffer(jsonBytes))
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
	assert.NotEmpty(t, w.Body.String())

	var resp testutils.APIResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)

	assert.NoError(t, err)
	assert.Empty(t, resp.Message)
	assert.NotEmpty(t, resp.Error)
	assert.Equal(t, resp.Error, "DMR ID is invalid")
}

func TestRegisterBadCallsign(t *testing.T) {
	router := testutils.CreateRouter()
	defer testutils.CloseRedis()
	defer testutils.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "N0CALL",
		Username: "n0call",
		Password: "password",
	}

	jsonBytes, err := json.Marshal(user)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/users", bytes.NewBuffer(jsonBytes))
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
	assert.NotEmpty(t, w.Body.String())

	var resp testutils.APIResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)

	assert.NoError(t, err)
	assert.Empty(t, resp.Message)
	assert.NotEmpty(t, resp.Error)
	assert.Equal(t, resp.Error, "Callsign does not match DMR ID")
}

func TestRegisterLowercaseCallsign(t *testing.T) {
	router := testutils.CreateRouter()
	defer testutils.CloseRedis()
	defer testutils.CloseDB()

	// Call this to load in the dbs
	userdb.GetDMRUsers()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "ki5vmf",
		Username: "n0call",
		Password: "password",
	}

	jsonBytes, err := json.Marshal(user)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/users", bytes.NewBuffer(jsonBytes))
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.NotEmpty(t, w.Body.String())

	var resp testutils.APIResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)

	assert.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.NotEmpty(t, resp.Message)
	assert.Equal(t, resp.Message, "User created, please wait for admin approval")
}

func TestRegisterUppercaseCallsign(t *testing.T) {
	router := testutils.CreateRouter()
	defer testutils.CloseRedis()
	defer testutils.CloseDB()

	// Call this to load in the dbs
	userdb.GetDMRUsers()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "n0call",
		Password: "password",
	}

	jsonBytes, err := json.Marshal(user)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/users", bytes.NewBuffer(jsonBytes))
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.NotEmpty(t, w.Body.String())

	var resp testutils.APIResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)

	assert.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.NotEmpty(t, resp.Message)
	assert.Equal(t, resp.Message, "User created, please wait for admin approval")
}
