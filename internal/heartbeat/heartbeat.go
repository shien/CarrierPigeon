// Package heartbeat implements liveness reporting without opening any
// listening port: the poller touches a file after every completed cycle
// and the container healthcheck verifies the file is recent.
package heartbeat

import (
	"fmt"
	"os"
	"time"
)

func Touch(path string) error {
	now := time.Now()
	if err := os.Chtimes(path, now, now); os.IsNotExist(err) {
		return os.WriteFile(path, []byte{}, 0o644)
	} else {
		return err
	}
}

// Check fails when the heartbeat file is missing or older than maxAge.
func Check(path string, maxAge time.Duration) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("heartbeat missing: %w", err)
	}
	if age := time.Since(info.ModTime()); age > maxAge {
		return fmt.Errorf("heartbeat stale: last cycle %s ago (max %s)", age.Round(time.Second), maxAge)
	}
	return nil
}
