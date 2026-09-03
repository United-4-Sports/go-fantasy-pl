// Package models defines the data structures used throughout the FPL SDK.
// These models map directly to the JSON responses from the official FPL API.
package models

import (
	"strconv"
)

// Player represents an FPL player (referred to as an "element" in the API).
// It contains summary data, current season performance, and ownership statistics.
type Player struct {
	ID                       int     `json:"id"`
	FirstName                string  `json:"first_name"`
	SecondName               string  `json:"second_name"`
	WebName                  string  `json:"web_name"`
	Team                     int     `json:"team"`
	TeamCode                 int     `json:"team_code"`
	TotalPoints              int     `json:"total_points"`
	NowCost                  float64 `json:"now_cost"`
	SelectedByPercent        string  `json:"selected_by_percent"`
	Form                     string  `json:"form"`
	InDreamteam              bool    `json:"in_dreamteam"`
	Minutes                  int     `json:"minutes"`
	GoalsScored              int     `json:"goals_scored"`
	Assists                  int     `json:"assists"`
	CleanSheets              int     `json:"clean_sheets"`
	YellowCards              int     `json:"yellow_cards"`
	RedCards                 int     `json:"red_cards"`
	Status                   string  `json:"status"`
	ChanceOfPlaying          float64 `json:"chance_of_playing_next_round"`
	Code                     int     `json:"code"`
	CostChangeEvent          int     `json:"cost_change_event"`
	CostChangeEventFall      int     `json:"cost_change_event_fall"`
	CostChangeStart          int     `json:"cost_change_start"`
	CostChangeStartFall      int     `json:"cost_change_start_fall"`
	DreamteamCount           int     `json:"dreamteam_count"`
	ElementType              int     `json:"element_type"`
	EpNext                   string  `json:"ep_next"`
	EpThis                   string  `json:"ep_this"`
	EventPoints              int     `json:"event_points"`
	News                     string  `json:"news"`
	NewsAdded                string  `json:"news_added"`
	PointsPerGame            string  `json:"points_per_game"`
	Special                  bool    `json:"special"`
	SquadNumber              *int    `json:"squad_number"`
	TransfersIn              int     `json:"transfers_in"`
	TransfersInEvent         int     `json:"transfers_in_event"`
	TransfersOut             int     `json:"transfers_out"`
	TransfersOutEvent        int     `json:"transfers_out_event"`
	ValueForm                string  `json:"value_form"`
	ValueSeason              string  `json:"value_season"`
	Influence                string  `json:"influence"`
	Creativity               string  `json:"creativity"`
	Threat                   string  `json:"threat"`
	IctIndex                 string  `json:"ict_index"`
	Starts                   int     `json:"starts"`
	ExpectedGoals            string  `json:"expected_goals"`
	ExpectedAssists          string  `json:"expected_assists"`
	ExpectedGoalInvolvements string  `json:"expected_goal_involvements"`
	ExpectedGoalsConceded    string  `json:"expected_goals_conceded"`
	NowCostRank              int     `json:"now_cost_rank"`
	NowCostRankType          int     `json:"now_cost_rank_type"`
	FormRank                 int     `json:"form_rank"`
	FormRankType             int     `json:"form_rank_type"`
	PointsPerGameRank        int     `json:"points_per_game_rank"`
	PointsPerGameRankType    int     `json:"points_per_game_rank_type"`
	SelectedRank             int     `json:"selected_rank"`
	SelectedRankType         int     `json:"selected_rank_type"`
}

// GetDisplayName returns the full name of the player.
func (p *Player) GetDisplayName() string {
	return p.FirstName + " " + p.SecondName
}

// GetPriceInPounds returns the current cost of the player in millions of pounds (e.g., 8.5).
func (p *Player) GetPriceInPounds() float64 {
	return p.NowCost / 10
}

// FPL Player availability status codes.
const (
	StatusAvailable   = "a" // Available
	StatusDoubtful    = "d" // Doubtful
	StatusInjured     = "i" // Injured
	StatusSuspended   = "s" // Suspended
	StatusUnavailable = "u" // Unavailable
	StatusNoSquad     = "n" // Not in squad
)

// IsAvailable reports whether the player is unflagged and available for selection ("a").
func (p *Player) IsAvailable() bool {
	return p.Status == StatusAvailable
}

// IsInjured reports whether the player is flagged with an injury ("i").
func (p *Player) IsInjured() bool {
	return p.Status == StatusInjured
}

// IsSuspended reports whether the player is currently suspended ("s").
func (p *Player) IsSuspended() bool {
	return p.Status == StatusSuspended
}

// IsDoubtful reports whether the player is flagged as doubtful ("d").
func (p *Player) IsDoubtful() bool {
	return p.Status == StatusDoubtful
}

func parseFloat(s string) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

// GetForm returns the player's recent form as a float64 (0.0 if empty or unparsable).
func (p *Player) GetForm() float64 {
	return parseFloat(p.Form)
}

// GetPointsPerGame returns the player's points per game as a float64 (0.0 if empty or unparsable).
func (p *Player) GetPointsPerGame() float64 {
	return parseFloat(p.PointsPerGame)
}

// GetEpNext returns FPL's expected points next round as a float64 (0.0 if empty or unparsable).
func (p *Player) GetEpNext() float64 {
	return parseFloat(p.EpNext)
}

// GetEpThis returns FPL's expected points this round as a float64 (0.0 if empty or unparsable).
func (p *Player) GetEpThis() float64 {
	return parseFloat(p.EpThis)
}

// GetIctIndex returns the player's ICT index as a float64 (0.0 if empty or unparsable).
func (p *Player) GetIctIndex() float64 {
	return parseFloat(p.IctIndex)
}

// GetSelectedByPercent returns the percentage of managers owning the player as a float64 (0.0 if empty or unparsable).
func (p *Player) GetSelectedByPercent() float64 {
	return parseFloat(p.SelectedByPercent)
}

// GetExpectedGoals returns the player's expected goals (xG) as a float64 (0.0 if empty or unparsable).
func (p *Player) GetExpectedGoals() float64 {
	return parseFloat(p.ExpectedGoals)
}

// GetExpectedAssists returns the player's expected assists (xA) as a float64 (0.0 if empty or unparsable).
func (p *Player) GetExpectedAssists() float64 {
	return parseFloat(p.ExpectedAssists)
}

// GetExpectedGoalInvolvements returns expected goal involvements (xGI) as a float64 (0.0 if empty or unparsable).
func (p *Player) GetExpectedGoalInvolvements() float64 {
	return parseFloat(p.ExpectedGoalInvolvements)
}

// GetExpectedGoalsConceded returns expected goals conceded (xGC) as a float64 (0.0 if empty or unparsable).
func (p *Player) GetExpectedGoalsConceded() float64 {
	return parseFloat(p.ExpectedGoalsConceded)
}

// Standard FPL position element types.
const (
	ElementTypeGoalkeeper = 1
	ElementTypeDefender   = 2
	ElementTypeMidfielder = 3
	ElementTypeForward    = 4
)

// PositionName returns the human-readable position name for an element type code ("goalkeeper", "defender", "midfielder", "forward", or "unknown").
func PositionName(elementType int) string {
	switch elementType {
	case ElementTypeGoalkeeper:
		return "goalkeeper"
	case ElementTypeDefender:
		return "defender"
	case ElementTypeMidfielder:
		return "midfielder"
	case ElementTypeForward:
		return "forward"
	default:
		return "unknown"
	}
}

// GetPositionName returns the human-readable position label for this player.
func (p *Player) GetPositionName() string {
	return PositionName(p.ElementType)
}

