package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

func TestReadConfigWithOptionFileFlag(t *testing.T) {
	path := filepath.Join(t.TempDir(), "options.json")
	if err := os.WriteFile(path, []byte(`{"AquareaServiceCloudURL":"https://example.com/","PoolInterval":"5s"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := flag.Set("optionFile", path); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = flag.Set("optionFile", "") }()

	config := readConfig()
	if config.AquareaServiceCloudURL != "https://example.com/" {
		t.Errorf("unexpected AquareaServiceCloudURL %q", config.AquareaServiceCloudURL)
	}
	if config.PoolInterval != "5s" {
		t.Errorf("unexpected PoolInterval %q", config.PoolInterval)
	}
}
