package tickengine

import "testing"

func TestCalculateDamage(t *testing.T) {
	tests := []struct {
		name          string
		attack        int
		defense       int
		wantMinAtLeast int // damage should never be below this
		wantMaxAtMost  int // damage should never exceed this
	}{
		{"attack exceeds defense", 15, 5, 9, 11},   // raw=10, +/-10% => 9..11
		{"defense equals attack", 10, 10, 0, 0},     // raw=0 => always 0
		{"defense exceeds attack", 5, 15, 0, 0},     // raw negative, floored to 0
		{"large numbers", 100, 20, 72, 88},          // raw=80, +/-10% => 72..88
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Run many times since variance is randomized — this is a
			// property test (result always within bounds), not a single
			// deterministic assertion.
			for i := 0; i < 200; i++ {
				got := calculateDamage(tt.attack, tt.defense)
				if got < 0 {
					t.Fatalf("damage went negative: %d", got)
				}
				if got < tt.wantMinAtLeast || got > tt.wantMaxAtMost {
					t.Fatalf("damage %d out of expected range [%d, %d] for attack=%d defense=%d",
						got, tt.wantMinAtLeast, tt.wantMaxAtMost, tt.attack, tt.defense)
				}
			}
		})
	}
}
