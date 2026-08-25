package models

import (
	"fmt"
	"time"
)

type ClassicLeague struct {
	NewEntries      NewEntries `json:"new_entries"`
	LastUpdatedData time.Time  `json:"last_updated_data"`
	League          League     `json:"league"`
	Standings       Standings  `json:"standings"`
}

type NewEntries struct {
	HasNext bool  `json:"has_next"`
	Page    int   `json:"page"`
	Number  int   `json:"number"`
	Results []any `json:"results"` // Assuming results can be of various types
}

// League represents the league details.
type League struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Created     time.Time `json:"created"`
	Closed      bool      `json:"closed"`
	MaxEntries  *int      `json:"max_entries"` // Pointer to allow null
	LeagueType  string    `json:"league_type"`
	Scoring     string    `json:"scoring"`
	AdminEntry  *int      `json:"admin_entry"` // Pointer to allow null
	StartEvent  int       `json:"start_event"`
	CodePrivacy string    `json:"code_privacy"`
	HasCup      bool      `json:"has_cup"`
	CupLeague   int       `json:"cup_league"`
	Rank        *int      `json:"rank"` // Pointer to allow null
}

// Standings represents the standings section of the Classic League.
type Standings struct {
	HasNext bool            `json:"has_next"`
	Page    int             `json:"page"`
	Number  int             `json:"number"`
	Results []LeagueManager `json:"results"`
}

// Player represents an individual player's standings information.
type LeagueManager struct {
	ID         int    `json:"id"`
	EventTotal int    `json:"event_total"`
	PlayerName string `json:"player_name"`
	Rank       int    `json:"rank"`
	LastRank   int    `json:"last_rank"`
	RankSort   int    `json:"rank_sort"`
	Total      int    `json:"total"`
	Entry      int    `json:"entry"`
	EntryName  string `json:"entry_name"`
	HasPlayed  bool   `json:"has_played"`
}

func (cl *ClassicLeague) GetLeagueInfo() string {
	return fmt.Sprintf("%s (ID: %d)", cl.League.Name, cl.League.ID)
}

func (cl *ClassicLeague) GetUpdateTime() string {
	return cl.LastUpdatedData.Format(time.RFC822)
}

func (cl *ClassicLeague) GetTopManagers(n int) []LeagueManager {
	if n > len(cl.Standings.Results) {
		n = len(cl.Standings.Results)
	}
	return cl.Standings.Results[:n]
}

// League methods
func (l *League) GetMaxEntries() int {
	if l.MaxEntries == nil {
		return 0
	}
	return *l.MaxEntries
}

func (l *League) GetAdminEntry() int {
	if l.AdminEntry == nil {
		return 0
	}
	return *l.AdminEntry
}

func (l *League) GetRank() int {
	if l.Rank == nil {
		return 0
	}
	return *l.Rank
}

func (l *League) GetCreationDate() string {
	return l.Created.Format("2006-01-02")
}

// LeagueManager methods
func (lm *LeagueManager) GetManagerInfo() string {
	return fmt.Sprintf("%s (%s)", lm.EntryName, lm.PlayerName)
}

func (lm *LeagueManager) GetRankChange() int {
	return lm.LastRank - lm.Rank
}

func (lm *LeagueManager) GetRankChangeString() string {
	change := lm.GetRankChange()
	if change > 0 {
		return fmt.Sprintf("↑%d", change)
	} else if change < 0 {
		return fmt.Sprintf("↓%d", -change)
	}
	return "→"
}

// Standings methods
func (s *Standings) GetPageInfo() string {
	currentPage := s.Page
	if currentPage == 0 {
		currentPage = s.Number
	}
	return fmt.Sprintf("Page %d", currentPage)
}

func (s *Standings) HasPreviousPage() bool {
	currentPage := s.Page
	if currentPage == 0 {
		currentPage = s.Number
	}
	return currentPage > 1
}

type H2HLeagueMatchesPage struct {
	HasNext bool       `json:"has_next"`
	Page    int        `json:"page"`
	Results []H2HMatch `json:"results"`
}

type H2HMatch struct {
	ID               int    `json:"id"`
	Entry1Entry      int    `json:"entry_1_entry"`
	Entry1Name       string `json:"entry_1_name"`
	Entry1PlayerName string `json:"entry_1_player_name"`
	Entry1Points     int    `json:"entry_1_points"`
	Entry1Win        int    `json:"entry_1_win"`
	Entry1Draw       int    `json:"entry_1_draw"`
	Entry1Loss       int    `json:"entry_1_loss"`
	Entry1Total      int    `json:"entry_1_total"`
	Entry2Entry      int    `json:"entry_2_entry"`
	Entry2Name       string `json:"entry_2_name"`
	Entry2PlayerName string `json:"entry_2_player_name"`
	Entry2Points     int    `json:"entry_2_points"`
	Entry2Win        int    `json:"entry_2_win"`
	Entry2Draw       int    `json:"entry_2_draw"`
	Entry2Loss       int    `json:"entry_2_loss"`
	Entry2Total      int    `json:"entry_2_total"`
	IsKnockout       bool   `json:"is_knockout"`
	League           int    `json:"league"`
	Winner           *int   `json:"winner"`
	SeedValue        *int   `json:"seed_value"`
	Event            int    `json:"event"`
	Tiebreak         *int   `json:"tiebreak"`
	IsBye            bool   `json:"is_bye"`
	KnockoutName     string `json:"knockout_name"`
}

type H2HLeagueStandings struct {
	NewEntries      H2HNewEntries `json:"new_entries"`
	LastUpdatedData *time.Time    `json:"last_updated_data"`
	League          H2HLeague     `json:"league"`
	Standings       H2HStandings  `json:"standings"`
}

type H2HNewEntries struct {
	HasNext bool  `json:"has_next"`
	Page    int   `json:"page"`
	Results []any `json:"results"`
}

type H2HLeague struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Created     time.Time `json:"created"`
	Closed      bool      `json:"closed"`
	MaxEntries  *int      `json:"max_entries"`
	LeagueType  string    `json:"league_type"`
	Scoring     string    `json:"scoring"`
	AdminEntry  *int      `json:"admin_entry"`
	StartEvent  int       `json:"start_event"`
	CodePrivacy string    `json:"code_privacy"`
	HasCup      bool      `json:"has_cup"`
	CupLeague   *int      `json:"cup_league"`
	KoRounds    *int      `json:"ko_rounds"`
}

type H2HStandings struct {
	HasNext bool                `json:"has_next"`
	Page    int                 `json:"page"`
	Results []H2HLeagueStanding `json:"results"`
}

type H2HLeagueStanding struct {
	ID            int    `json:"id"`
	EntryName     string `json:"entry_name"`
	PlayerName    string `json:"player_name"`
	Rank          int    `json:"rank"`
	LastRank      int    `json:"last_rank"`
	RankSort      int    `json:"rank_sort"`
	Total         int    `json:"total"`
	MatchesPlayed int    `json:"matches_played"`
	MatchesWon    int    `json:"matches_won"`
	MatchesDrawn  int    `json:"matches_drawn"`
	MatchesLost   int    `json:"matches_lost"`
	PointsFor     int    `json:"points_for"`
	Division      int    `json:"division"`
	Entry         int    `json:"entry"`
}
