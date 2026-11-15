package domain

import "github.com/google/uuid"

// Standing is one row of the leaderboard.
type Standing struct {
	PlayerID    uuid.UUID
	DisplayName string
	Credits     int64
	Planets     int
	Ships       int
	Kills       int
	Captures    int
	Damage      int64
	Score       int64
}

// ComputeScore turns a player's holdings and record into a single number.
//
//	100 per planet owned
//	 10 per ship alive
//	 50 per enemy fleet destroyed
//	 75 per enemy planet captured
//	  1 per 10 damage dealt
//	  1 per 100 credits banked
func ComputeScore(planets, ships, kills, captures int, damage, credits int64) int64 {
	return int64(planets)*100 + int64(ships)*10 + int64(kills)*50 + int64(captures)*75 + damage/10 + credits/100
}
