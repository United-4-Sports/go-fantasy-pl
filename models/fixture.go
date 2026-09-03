package models

import (
	"fmt"
	"time"
)

// Standard identifiers for match statistics.
const (
	StatGoalsScored     = "goals_scored"
	StatAssists         = "assists"
	StatOwnGoals        = "own_goals"
	StatYellowCards     = "yellow_cards"
	StatRedCards        = "red_cards"
	StatPenaltiesSaved  = "penalties_saved"
	StatPenaltiesMissed = "penalties_missed"
	StatSaves           = "saves"
	StatBonus           = "bonus"
)

// Stat represents a specific type of match statistic (e.g., goals)
// and the players from each team who contributed to it.
type Stat struct {
	Identifier string       `json:"identifier"`
	A          []StatDetail `json:"a"` // Away team contributors
	H          []StatDetail `json:"h"` // Home team contributors
}

// StatDetail contains the specific value and player ID for a match statistic.
type StatDetail struct {
	Value   int `json:"value"`
	Element int `json:"element"`
}

// Fixture represents a scheduled match between two Premier League teams.
// It includes kickoff times, scores, and detailed match statistics.
type Fixture struct {
	Code                 int        `json:"code"`
	Event                *int       `json:"event"`
	Finished             bool       `json:"finished"`
	FinishedProvisional  bool       `json:"finished_provisional"`
	ID                   int        `json:"id"`
	KickoffTime          *time.Time `json:"kickoff_time"`
	Minutes              int        `json:"minutes"`
	ProvisionalStartTime bool       `json:"provisional_start_time"`
	Started              bool       `json:"started"`
	TeamA                int        `json:"team_a"`
	TeamAScore           *int       `json:"team_a_score"`
	TeamH                int        `json:"team_h"`
	TeamHScore           *int       `json:"team_h_score"`
	Stats                []Stat     `json:"stats"`
	TeamHDifficulty      int        `json:"team_h_difficulty"`
	TeamADifficulty      int        `json:"team_a_difficulty"`
	PulseID              int        `json:"pulse_id"`
}

// GetTeamAScore returns the away team's score, or 0 if the match hasn't started or score is unavailable.
func (f *Fixture) GetTeamAScore() int {
	if f.TeamAScore == nil {
		return 0
	}
	return *f.TeamAScore
}

// GetTeamHScore returns the home team's score, or 0 if the match hasn't started or score is unavailable.
func (f *Fixture) GetTeamHScore() int {
	if f.TeamHScore == nil {
		return 0
	}
	return *f.TeamHScore
}

func (f *Fixture) getStat(identifier string) (map[string][]StatDetail, error) {
	result := make(map[string][]StatDetail)
	for _, stat := range f.Stats {
		if stat.Identifier == identifier {
			result["a"] = stat.A
			result["h"] = stat.H
			return result, nil
		}
	}
	return result, nil
}

// GetGoalscorers returns players who scored goals in the fixture.
func (f *Fixture) GetGoalscorers() (map[string][]StatDetail, error) {
	return f.getStat(StatGoalsScored)
}

// GetAssisters returns players who provided assists in the fixture.
func (f *Fixture) GetAssisters() (map[string][]StatDetail, error) {
	return f.getStat(StatAssists)
}

// GetOwnGoalscorers returns players who scored own goals in the fixture.
func (f *Fixture) GetOwnGoalscorers() (map[string][]StatDetail, error) {
	return f.getStat(StatOwnGoals)
}

// GetYellowCards returns players who received yellow cards.
func (f *Fixture) GetYellowCards() (map[string][]StatDetail, error) {
	return f.getStat(StatYellowCards)
}

// GetRedCards returns players who received red cards.
func (f *Fixture) GetRedCards() (map[string][]StatDetail, error) {
	return f.getStat(StatRedCards)
}

// GetPenaltySaves returns goalkeepers who saved penalties.
func (f *Fixture) GetPenaltySaves() (map[string][]StatDetail, error) {
	return f.getStat(StatPenaltiesSaved)
}

// GetPenaltyMisses returns players who missed penalties.
func (f *Fixture) GetPenaltyMisses() (map[string][]StatDetail, error) {
	return f.getStat(StatPenaltiesMissed)
}

// GetSaves returns goalkeepers and their save counts.
func (f *Fixture) GetSaves() (map[string][]StatDetail, error) {
	return f.getStat(StatSaves)
}

// GetBonus returns players who received bonus points.
func (f *Fixture) GetBonus() (map[string][]StatDetail, error) {
	return f.getStat(StatBonus)
}

// OpponentID returns the opponent team ID for the given team in this fixture.
// Returns 0 if the team is not playing in this match.
func (f *Fixture) OpponentID(teamID int) int {
	if f.TeamH == teamID {
		return f.TeamA
	}
	if f.TeamA == teamID {
		return f.TeamH
	}
	return 0
}

// IsHome reports whether the given team is playing at home in this fixture.
func (f *Fixture) IsHome(teamID int) bool {
	return f.TeamH == teamID
}

// DifficultyFor returns the fixture difficulty rating (1..5) for the given team.
// Returns 0 if the team is not playing in this match.
func (f *Fixture) DifficultyFor(teamID int) int {
	if f.TeamH == teamID {
		return f.TeamHDifficulty
	}
	if f.TeamA == teamID {
		return f.TeamADifficulty
	}
	return 0
}

// OpponentDescriptor returns a formatted opponent description with venue indicator, e.g. "Chelsea (H)" or "Arsenal (A)".
// If the given team is not playing in this match, it returns an empty string.
func (f *Fixture) OpponentDescriptor(teamID int, opponentName string) string {
	if f.TeamH != teamID && f.TeamA != teamID {
		return ""
	}
	venue := "A"
	if f.IsHome(teamID) {
		venue = "H"
	}
	return fmt.Sprintf("%s (%s)", opponentName, venue)
}

