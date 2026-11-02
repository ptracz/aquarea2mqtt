package main

import (
	"errors"
	"testing"
)

func TestParseResponseJSON(t *testing.T) {
	aq := &aquarea{}
	var out aquareaLoginJSON
	if err := aq.parseResponse([]byte(`{"errorCode":0}`), &out); err != nil {
		t.Fatalf("parseResponse: %v", err)
	}
}

func TestParseResponseHTMLIsSessionExpired(t *testing.T) {
	aq := &aquarea{}
	var out aquareaLoginJSON
	err := aq.parseResponse([]byte("<!DOCTYPE html><html><head><title>Login</title></head><body></body></html>"), &out)
	if !errors.Is(err, errSessionExpired) {
		t.Fatalf("expected errSessionExpired, got %v", err)
	}
}

func TestParseResponseIndentedHTMLIsSessionExpired(t *testing.T) {
	aq := &aquarea{}
	var out aquareaLoginJSON
	err := aq.parseResponse([]byte("\n\t<html><body>login page</body></html>"), &out)
	if !errors.Is(err, errSessionExpired) {
		t.Fatalf("expected errSessionExpired, got %v", err)
	}
}

func TestParseResponseInvalidJSONIsNotSessionExpired(t *testing.T) {
	aq := &aquarea{}
	var out aquareaLoginJSON
	err := aq.parseResponse([]byte(`{broken`), &out)
	if err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
	if errors.Is(err, errSessionExpired) {
		t.Fatalf("invalid JSON should not be reported as session expired: %v", err)
	}
}
