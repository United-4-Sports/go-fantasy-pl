package models

import "time"

// ManagerHistory represents the history of a manager's performance.
type ManagerHistory struct {
	Current []CurrentEvents `json:"current"` // Current gameweek events
	Past    []PastSeason    `json:"past"`    // Past seasons
	Chips   []Chip          `json:"chips"`   // Chips used
}

// CurrentEvent represents the details of this year's gameweek events.
type CurrentEvents struct {
	Event              int `json:"event"`                // Gameweek ID
	Points             int `json:"points"`               // Points scored in the gameweek
	TotalPoints        int `json:"total_points"`         // Total points accumulated
	Rank               int `json:"rank"`                 // Current rank
	RankSort           int `json:"rank_sort"`            // Rank sort
	OverallRank        int `json:"overall_rank"`         // Overall rank
	PercentileRank     int `json:"percentile_rank"`      // Percentile rank
	Bank               int `json:"bank"`                 // Money in bank
	Value              int `json:"value"`                // Team value
	EventTransfers     int `json:"event_transfers"`      // Transfers made in the gameweek
	EventTransfersCost int `json:"event_transfers_cost"` // Cost of transfers
	PointsOnBench      int `json:"points_on_bench"`      // Points scored by bench players
}

// GetTeamValueInMillions returns the team value in millions of pounds (e.g. 100.5).
func (c *CurrentEvents) GetTeamValueInMillions() float64 {
	return float64(c.Value) / 10
}

// GetBankValueInMillions returns the bank balance in millions of pounds (e.g. 1.2).
func (c *CurrentEvents) GetBankValueInMillions() float64 {
	return float64(c.Bank) / 10
}

// PastSeason represents the details of a past season.
type PastSeason struct {
	SeasonName  string `json:"season_name"`  // Name of the season (e.g., "2020/21")
	TotalPoints int    `json:"total_points"` // Total points scored in the season
	Rank        int    `json:"rank"`         // Rank achieved in the season
}

// Chip represents the details of a chip used by the manager.
type Chip struct {
	Name  string    `json:"name"`  // Name of the chip (e.g., "wildcard")
	Time  time.Time `json:"time"`  // Time when the chip was used
	Event int       `json:"event"` // Gameweek ID when the chip was used
}
