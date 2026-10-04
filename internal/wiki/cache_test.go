package wiki

import (
	"database/sql"
	"os"
	"testing"
)

// A cache another process holds locked is reported, not deleted.
func TestOpenKeepsALockedCache(t *testing.T) {
	w, _ := fixture(t)
	w.Close()
	defer func(ms int) { busyTimeout = ms }(busyTimeout)
	busyTimeout = 50

	before, err := os.Stat(w.Cache())
	if err != nil {
		t.Fatal(err)
	}
	other, err := sql.Open("sqlite", w.Cache())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	other.SetMaxOpenConns(1)
	if _, err := other.Exec(`BEGIN IMMEDIATE`); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(w.Project); err == nil {
		t.Fatal("Open succeeded while another writer held the cache")
	}
	if after, err := os.Stat(w.Cache()); err != nil || !os.SameFile(before, after) {
		t.Fatalf("the locked cache was replaced: %v", err)
	}
}
