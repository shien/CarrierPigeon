// Package watcher defines the interface every notification source must
// implement. Adding a new service (Twitch, niconico, ...) means providing
// another implementation and wiring it up in main.
package watcher

import "context"

type Watcher interface {
	// Run blocks and polls until ctx is cancelled.
	Run(ctx context.Context) error
	// Seed registers a channel's current content as already-notified so
	// that subscribing never floods Discord with past uploads.
	Seed(ctx context.Context, channelID string) error
}
