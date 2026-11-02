package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

const (
	fakeModeOK         = iota
	fakeModeLogExpired // data/log answers with the 3000-0002 session-expired message
	fakeModeHTML       // setting/get answers with an HTML login page
	fakeModeLogError   // data/log answers with a plain server error
)

// fakeAquareaCloud is a stand-in for the Panasonic cloud API. mode selects the
// behavior of the data endpoints so tests can reproduce what the real cloud
// returns after a session expires
type fakeAquareaCloud struct {
	mode       atomic.Int32
	loginCount atomic.Int32
}

func (f *fakeAquareaCloud) handler(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/installer/api/auth/login":
		f.loginCount.Add(1)
		w.Write([]byte(`{"errorCode":0}`))
	case "/installer/api/endusers":
		w.Write([]byte(`{"errorCode":0,"endusers":[{"gwid":"gw1","gwUid":"gw1","deviceId":"dev1","name":"Test"}]}`))
	case "/installer/functionUserInformation":
		// HTML page route (no /api/ prefix) - answers with a page even when
		// the session is fine, selectDevice must not treat it as expired
		w.Write([]byte("<!DOCTYPE html><html><head><title>Device</title></head></html>"))
	case "/installer/api/function/setting/get":
		if f.mode.Load() == fakeModeHTML {
			w.Write([]byte("<!DOCTYPE html><html><head><title>Login</title></head></html>"))
			return
		}
		w.Write([]byte(`{"errorCode":0,"settingDataInfo":{},"settingBackgroundData":{}}`))
	case "/installer/api/function/status":
		w.Write([]byte(`{"errorCode":0,"statusDataInfo":{}}`))
	case "/installer/api/data/log":
		switch f.mode.Load() {
		case fakeModeLogExpired:
			w.Write([]byte(sessionExpiredLogResponse))
		case fakeModeLogError:
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("oops"))
		default:
			w.Write([]byte(`{"errorCode":0}`))
		}
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func newTestAquarea(url string) *aquarea {
	return &aquarea{
		AquareaServiceCloudURL: url,
		dataChannel:            make(chan map[string]string, 100),
		statusChannel:          make(chan bool, 100),
		httpClient:             http.Client{},
		usersMap:               make(map[string]aquareaEndUserJSON),
		logItems:               []aquareaLogItem{{Name: "LogItem0"}},
	}
}

// drainStatuses returns and empties the status channel
func (aq *aquarea) drainStatuses() []bool {
	var statuses []bool
	for len(aq.statusChannel) > 0 {
		statuses = append(statuses, <-aq.statusChannel)
	}
	return statuses
}

// setupLoggedinAquarea returns an aquarea instance logged into a fake cloud
// (one successful login, usersMap populated, status channel drained)
func setupLoggedinAquarea(t *testing.T) (*aquarea, *fakeAquareaCloud) {
	t.Helper()
	fake := &fakeAquareaCloud{}
	ts := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(ts.Close)

	aq := newTestAquarea(ts.URL)
	if !aq.aquareaSetup() {
		t.Fatal("initial aquareaSetup failed")
	}
	if _, ok := aq.usersMap["gw1"]; !ok {
		t.Fatalf("usersMap not populated by endusers fetch: %+v", aq.usersMap)
	}
	aq.drainStatuses() // drop the "online" announcement of the initial login
	return aq, fake
}

// Reproduction of the 2026-11-01 log: the cloud answers data/log with
// "Your session has expired" (3000-0002) while other endpoints still work
func TestFeedDataFromAquareaReLoginOnSessionExpired(t *testing.T) {
	aq, fake := setupLoggedinAquarea(t)
	fake.mode.Store(fakeModeLogExpired)

	aq.feedDataFromAquarea()

	if got := fake.loginCount.Load(); got != 2 {
		t.Fatalf("expected a re-login after session expiry, login endpoint called %d times", got)
	}
	statuses := aq.drainStatuses()
	if len(statuses) != 2 || statuses[0] || !statuses[1] {
		t.Fatalf("expected [offline online] status sequence, got %v", statuses)
	}
}

// Reproduction of the 2026-11-02 log: the cloud starts answering with the
// HTML login page instead of JSON
func TestFeedDataFromAquareaReLoginOnLoginPage(t *testing.T) {
	aq, fake := setupLoggedinAquarea(t)
	fake.mode.Store(fakeModeHTML)

	aq.feedDataFromAquarea()

	if got := fake.loginCount.Load(); got != 2 {
		t.Fatalf("expected a re-login after receiving the login page, login endpoint called %d times", got)
	}
	statuses := aq.drainStatuses()
	if len(statuses) != 2 || statuses[0] || !statuses[1] {
		t.Fatalf("expected [offline online] status sequence, got %v", statuses)
	}
}

// A transient (non-authentication) server error must be logged and retried on
// the next poll - it must not trigger a re-login or an offline announcement
func TestFeedDataFromAquareaTransientErrorNoReLogin(t *testing.T) {
	aq, fake := setupLoggedinAquarea(t)
	fake.mode.Store(fakeModeLogError)

	aq.feedDataFromAquarea()

	if got := fake.loginCount.Load(); got != 1 {
		t.Fatalf("a transient server error must not trigger a re-login, login endpoint called %d times", got)
	}
	if statuses := aq.drainStatuses(); len(statuses) != 0 {
		t.Fatalf("a transient server error must not change the online status, got %v", statuses)
	}
}
