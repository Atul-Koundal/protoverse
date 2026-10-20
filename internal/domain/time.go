package domain

import "time"

// TickInterval is how often the game world advances. Every queued command
// resolves on a tick boundary.
const TickInterval = 10 * time.Second

// TickNumber converts a wall-clock time into a monotonically increasing tick
// counter shared by every service (and every tick-engine replica).
func TickNumber(t time.Time) int64 {
	return t.Unix() / int64(TickInterval/time.Second)
}

// ExecuteAfterTicks returns the time that is n whole ticks after now.
func ExecuteAfterTicks(now time.Time, n int) time.Time {
	return now.Add(time.Duration(n) * TickInterval)
}
