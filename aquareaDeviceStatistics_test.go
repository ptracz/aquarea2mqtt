package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Same payload the cloud sent in the 2026-11-01 log when the 24h session expired
const sessionExpiredLogResponse = `{"errorCode":0,"message":[{"errorMessage":"Your session has expired. Please login again.","errorCode":"3000-0002"}]}`

func TestGetDeviceLogInformationSessionExpired(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sessionExpiredLogResponse))
	}))
	defer ts.Close()

	_, err := newTestAquarea(ts.URL).getDeviceLogInformation(aquareaEndUserJSON{Gwid: "gw1", DeviceID: "dev1"})
	if !errors.Is(err, errSessionExpired) {
		t.Fatalf("expected errSessionExpired, got %v", err)
	}
}

func TestGetDeviceLogInformationOK(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"errorCode":0,"logData":"{\"1650000000\":[\"1\",\"5\"]}"}`))
	}))
	defer ts.Close()

	aq := newTestAquarea(ts.URL)
	aq.logItems = []aquareaLogItem{{Name: "Item0", Unit: "kWh"}, {Name: "Item1"}}
	stats, err := aq.getDeviceLogInformation(aquareaEndUserJSON{Gwid: "gw1", DeviceID: "dev1"})
	if err != nil {
		t.Fatalf("getDeviceLogInformation: %v", err)
	}
	if stats["aquarea/gw1/log/Item0"] != "1" {
		t.Errorf("unexpected Item0 value %q", stats["aquarea/gw1/log/Item0"])
	}
	if stats["aquarea/gw1/log/Item0/unit"] != "kWh" {
		t.Errorf("unexpected Item0 unit %q", stats["aquarea/gw1/log/Item0/unit"])
	}
	if stats["aquarea/gw1/log/Item1"] != "5" {
		t.Errorf("unexpected Item1 value %q", stats["aquarea/gw1/log/Item1"])
	}
	if stats["aquarea/gw1/log/Timestamp"] != "1650000000" {
		t.Errorf("unexpected timestamp %q", stats["aquarea/gw1/log/Timestamp"])
	}
}
