package models

import (
	"time"
)

// ChipPlay represents statistics for a specific chip (e.g., "wildcard", "triple_captain")
// played during a gameweek.
type ChipPlay struct {
	ChipName  string `json:"chip_name"`
	NumPlayed int    `json:"num_played"`
}

// TopElementInfo identifies the highest-scoring player for a specific gameweek.
type TopElementInfo struct {
	ID     int `json:"id"`
	Points int `json:"points"`
}

// GameWeek represents an FPL gameweek (referred to as an "event" in the API).
// It includes deadlines, scoring averages, and status flags.
type GameWeek struct {
	ID                     int            `json:"id"`
	Name                   string         `json:"name"`
	DeadlineTime           time.Time      `json:"deadline_time"`
	ReleaseTime            *time.Time     `json:"release_time"` // Pointer to handle null
	AverageEntryScore      int            `json:"average_entry_score"`
	Finished               bool           `json:"finished"`
	DataChecked            bool           `json:"data_checked"`
	HighestScoringEntry    int            `json:"highest_scoring_entry"`
	DeadlineTimeEpoch      int64          `json:"deadline_time_epoch"`
	DeadlineTimeGameOffset int            `json:"deadline_time_game_offset"`
	HighestScore           int            `json:"highest_score"`
	IsPrevious             bool           `json:"is_previous"`
	IsCurrent              bool           `json:"is_current"`
	IsNext                 bool           `json:"is_next"`
	CupLeaguesCreated      bool           `json:"cup_leagues_created"`
	H2hKoMatchesCreated    bool           `json:"h2h_ko_matches_created"`
	RankedCount            int            `json:"ranked_count"`
	ChipPlays              []ChipPlay     `json:"chip_plays"`
	MostSelected           int            `json:"most_selected"`
	MostTransferredIn      int            `json:"most_transferred_in"`
	TopElement             int            `json:"top_element"`
	TopElementInfo         TopElementInfo `json:"top_element_info"`
	TransfersMade          int            `json:"transfers_made"`
	MostCaptained          int            `json:"most_captained"`
	MostViceCaptained      int            `json:"most_vice_captained"`
}

// GetChipPlayCount returns the number of times a specific chip was played during the gameweek.
func (gw *GameWeek) GetChipPlayCount(chipName string) int {
	for _, chipPlay := range gw.ChipPlays {
		if chipPlay.ChipName == chipName {
			return chipPlay.NumPlayed
		}
	}
	return 0
}

// IsFinished returns true if the gameweek's matches have all concluded and data is finalized.
func (gw *GameWeek) IsFinished() bool {
	return gw.Finished
}

// GetTopElementInfo returns details about the top-scoring player of the gameweek.
func (gw *GameWeek) GetTopElementInfo() TopElementInfo {
	return gw.TopElementInfo
}

// IsActive returns true if the gameweek is currently underway (started and not yet finished).
func (gw *GameWeek) IsActive() bool {
	return gw.IsCurrent && !gw.Finished
}

// IsUpcoming returns true if the gameweek is scheduled for the future (is_next or unstarted).
func (gw *GameWeek) IsUpcoming() bool {
	return gw.IsNext || (!gw.IsPrevious && !gw.IsCurrent && !gw.Finished)
}

// DeadlinePassed reports whether the gameweek's deadline has already passed.
func (gw *GameWeek) DeadlinePassed() bool {
	if gw.DeadlineTime.IsZero() {
		return false
	}
	return time.Now().UTC().After(gw.DeadlineTime)
}
