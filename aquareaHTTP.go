package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
)

// errSessionExpired indicates the cloud session is no longer valid (the server
// answered with a login page or a session-expired error) and we must log in again
var errSessionExpired = errors.New("aquarea session expired")

// parseResponse decodes a JSON response body. When the server answers with an
// HTML page - typically the login page after the session has expired - it
// returns errSessionExpired instead of a JSON unmarshal error
func (aq *aquarea) parseResponse(b []byte, v interface{}) error {
	if s := bytes.TrimSpace(b); len(s) > 0 && s[0] == '<' {
		return errSessionExpired
	}
	return json.Unmarshal(b, v)
}

// Posts data to Aquarea web service
func (aq *aquarea) httpPost(url string, urlValues url.Values) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(urlValues.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Cache-Control", "max-age=0")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:74.0) Gecko/20100101 Firefox/74.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := aq.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	b, err := ioutil.ReadAll(resp.Body)
	return b, err
}

func (aq *aquarea) httpGet(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Cache-Control", "max-age=0")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:74.0) Gecko/20100101 Firefox/74.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Upgrade-Insecure-Requests", "1")

	resp, err := aq.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	b, err := ioutil.ReadAll(resp.Body)
	return b, err
}
