package repository

import (
	"sort"

	"github.com/Atul-Koundal/protoverse/internal/domain"
)

// sortStandings orders the leaderboard: highest score first, with ties broken
// by name so the order is stable between requests.
func sortStandings(s []domain.Standing) {
	sort.SliceStable(s, func(i, j int) bool {
		if s[i].Score != s[j].Score {
			return s[i].Score > s[j].Score
		}
		return s[i].DisplayName < s[j].DisplayName
	})
}
