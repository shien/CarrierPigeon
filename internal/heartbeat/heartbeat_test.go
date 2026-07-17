package heartbeat

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTouchAndCheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "heartbeat")

	if err := Check(path, time.Minute); err == nil {
		t.Error("Check on missing file must fail")
	}

	if err := Touch(path); err != nil {
		t.Fatalf("first touch (create): %v", err)
	}
	if err := Touch(path); err != nil {
		t.Fatalf("second touch (update): %v", err)
	}
	if err := Check(path, time.Minute); err != nil {
		t.Errorf("fresh heartbeat reported unhealthy: %v", err)
	}
}

func TestCheckStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "heartbeat")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if err := Check(path, time.Minute); err == nil {
		t.Error("stale heartbeat reported healthy")
	}
}
