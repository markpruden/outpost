package main

import (
	"testing"
	"time"
)

// A node that disconnected keeps its last-seen time through a controller restart.
func TestLastSeenSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	c := &Controller{dataDir: dir, nodes: map[string]*NodeRec{"n1": {ID: "n1"}, "n2": {ID: "n2"}},
		lastSeen: map[string]time.Time{}, conns: map[string]*nodeConn{}}
	seen := time.Now().Add(-time.Hour).Truncate(time.Second)

	c.mu.Lock()
	c.lastSeen["n1"] = seen
	c.saveLastSeenLocked("n1", 0) // what a disconnect does
	c.mu.Unlock()

	c2 := &Controller{dataDir: dir, nodes: map[string]*NodeRec{}, lastSeen: map[string]time.Time{}}
	if err := c2.loadNodes(); err != nil {
		t.Fatal(err)
	}
	c2.mu.Lock()
	defer c2.mu.Unlock()
	if got, ok := c2.lastSeenOfLocked("n1"); !ok || !got.Equal(seen) {
		t.Fatalf("n1 last seen = %v, %v; want %v", got, ok, seen)
	}
	if _, ok := c2.lastSeenOfLocked("n2"); ok {
		t.Fatal("n2 never connected but has a last-seen time")
	}
	if len(c2.lastSeen) != 0 {
		t.Fatal("the in-memory map must stay empty so no offline alerts fire at start-up")
	}
	// A fresh connection in this run wins over the saved time.
	now := time.Now()
	c2.lastSeen["n1"] = now
	if got, _ := c2.lastSeenOfLocked("n1"); !got.Equal(now) {
		t.Fatal("this run's time should win")
	}
}
