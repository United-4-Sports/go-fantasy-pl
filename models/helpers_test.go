package models

import (
	"testing"
	"time"
)

func TestPlayerHelpers(t *testing.T) {
	cases := []struct {
		name      string
		player    Player
		available bool
		injured   bool
		suspended bool
		doubtful  bool
	}{
		{
			name:      "fully fit available",
			player:    Player{Status: StatusAvailable, ChanceOfPlaying: 100},
			available: true,
		},
		{
			name:      "available with null chance",
			player:    Player{Status: StatusAvailable, ChanceOfPlaying: 0},
			available: true,
		},
		{
			name:      "doubtful with 75% chance",
			player:    Player{Status: StatusDoubtful, ChanceOfPlaying: 75},
			available: false,
			doubtful:  true,
		},
		{
			name:      "doubtful with 25% chance",
			player:    Player{Status: StatusDoubtful, ChanceOfPlaying: 25},
			available: false,
			doubtful:  true,
		},
		{
			name:      "doubtful with zero chance",
			player:    Player{Status: StatusDoubtful, ChanceOfPlaying: 0},
			available: false,
			doubtful:  true,
		},
		{
			name:      "available with 50% chance",
			player:    Player{Status: StatusAvailable, ChanceOfPlaying: 50},
			available: true,
		},
		{
			name:      "injured",
			player:    Player{Status: StatusInjured, ChanceOfPlaying: 0},
			available: false,
			injured:   true,
		},
		{
			name:      "suspended",
			player:    Player{Status: StatusSuspended, ChanceOfPlaying: 0},
			available: false,
			suspended: true,
		},
		{
			name:      "unavailable / out of squad",
			player:    Player{Status: StatusNoSquad, ChanceOfPlaying: 0},
			available: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.player.IsAvailable(); got != tc.available {
				t.Errorf("IsAvailable() = %v, want %v", got, tc.available)
			}
			if got := tc.player.IsInjured(); got != tc.injured {
				t.Errorf("IsInjured() = %v, want %v", got, tc.injured)
			}
			if got := tc.player.IsSuspended(); got != tc.suspended {
				t.Errorf("IsSuspended() = %v, want %v", got, tc.suspended)
			}
			if got := tc.player.IsDoubtful(); got != tc.doubtful {
				t.Errorf("IsDoubtful() = %v, want %v", got, tc.doubtful)
			}
		})
	}
}

func TestPlayerNumericGetters(t *testing.T) {
	p := Player{
		Form:                     "7.5",
		PointsPerGame:            "6.2",
		EpNext:                   "5.8",
		EpThis:                   "4.2",
		IctIndex:                 "120.4",
		SelectedByPercent:        "23.5",
		ExpectedGoals:            "0.85",
		ExpectedAssists:          "0.45",
		ExpectedGoalInvolvements: "1.30",
		ExpectedGoalsConceded:    "0.20",
	}

	if got := p.GetForm(); got != 7.5 {
		t.Errorf("GetForm() = %v, want 7.5", got)
	}
	if got := p.GetPointsPerGame(); got != 6.2 {
		t.Errorf("GetPointsPerGame() = %v, want 6.2", got)
	}
	if got := p.GetEpNext(); got != 5.8 {
		t.Errorf("GetEpNext() = %v, want 5.8", got)
	}
	if got := p.GetEpThis(); got != 4.2 {
		t.Errorf("GetEpThis() = %v, want 4.2", got)
	}
	if got := p.GetIctIndex(); got != 120.4 {
		t.Errorf("GetIctIndex() = %v, want 120.4", got)
	}
	if got := p.GetSelectedByPercent(); got != 23.5 {
		t.Errorf("GetSelectedByPercent() = %v, want 23.5", got)
	}
	if got := p.GetExpectedGoals(); got != 0.85 {
		t.Errorf("GetExpectedGoals() = %v, want 0.85", got)
	}
	if got := p.GetExpectedAssists(); got != 0.45 {
		t.Errorf("GetExpectedAssists() = %v, want 0.45", got)
	}
	if got := p.GetExpectedGoalInvolvements(); got != 1.30 {
		t.Errorf("GetExpectedGoalInvolvements() = %v, want 1.30", got)
	}
	if got := p.GetExpectedGoalsConceded(); got != 0.20 {
		t.Errorf("GetExpectedGoalsConceded() = %v, want 0.20", got)
	}

	// Test unparseable/empty fallback
	empty := Player{Form: "", PointsPerGame: "invalid"}
	if got := empty.GetForm(); got != 0.0 {
		t.Errorf("empty GetForm() = %v, want 0.0", got)
	}
	if got := empty.GetPointsPerGame(); got != 0.0 {
		t.Errorf("invalid GetPointsPerGame() = %v, want 0.0", got)
	}
}

func TestPositionHelpers(t *testing.T) {
	if got := PositionName(ElementTypeGoalkeeper); got != "goalkeeper" {
		t.Errorf("PositionName(GK) = %q, want goalkeeper", got)
	}
	if got := PositionName(ElementTypeDefender); got != "defender" {
		t.Errorf("PositionName(DEF) = %q, want defender", got)
	}
	if got := PositionName(ElementTypeMidfielder); got != "midfielder" {
		t.Errorf("PositionName(MID) = %q, want midfielder", got)
	}
	if got := PositionName(ElementTypeForward); got != "forward" {
		t.Errorf("PositionName(FWD) = %q, want forward", got)
	}
	if got := PositionName(999); got != "unknown" {
		t.Errorf("PositionName(999) = %q, want unknown", got)
	}

	p := Player{ElementType: ElementTypeMidfielder}
	if got := p.GetPositionName(); got != "midfielder" {
		t.Errorf("p.GetPositionName() = %q, want midfielder", got)
	}
}

func TestFixtureHelpers(t *testing.T) {
	f := Fixture{
		TeamH:           1, // Arsenal
		TeamA:           2, // Aston Villa
		TeamHDifficulty: 2,
		TeamADifficulty: 4,
	}

	if opp := f.OpponentID(1); opp != 2 {
		t.Errorf("OpponentID(1) = %d, want 2", opp)
	}
	if opp := f.OpponentID(2); opp != 1 {
		t.Errorf("OpponentID(2) = %d, want 1", opp)
	}
	if opp := f.OpponentID(3); opp != 0 {
		t.Errorf("OpponentID(3) = %d, want 0", opp)
	}

	if !f.IsHome(1) {
		t.Errorf("IsHome(1) = false, want true")
	}
	if f.IsHome(2) {
		t.Errorf("IsHome(2) = true, want false")
	}

	if diff := f.DifficultyFor(1); diff != 2 {
		t.Errorf("DifficultyFor(1) = %d, want 2", diff)
	}
	if diff := f.DifficultyFor(2); diff != 4 {
		t.Errorf("DifficultyFor(2) = %d, want 4", diff)
	}
	if diff := f.DifficultyFor(3); diff != 0 {
		t.Errorf("DifficultyFor(3) = %d, want 0", diff)
	}

	if got := f.OpponentDescriptor(1, "Opponent A"); got != "Opponent A (H)" {
		t.Errorf("OpponentDescriptor(1) = %q, want 'Opponent A (H)'", got)
	}
	if got := f.OpponentDescriptor(2, "Opponent B"); got != "Opponent B (A)" {
		t.Errorf("OpponentDescriptor(2) = %q, want 'Opponent B (A)'", got)
	}
	if got := f.OpponentDescriptor(3, "Opponent C"); got != "" {
		t.Errorf("OpponentDescriptor(3) = %q, want ''", got)
	}
}

func TestGameWeekHelpers(t *testing.T) {
	gwActive := GameWeek{IsCurrent: true, Finished: false}
	if !gwActive.IsActive() {
		t.Errorf("IsActive() = false, want true")
	}

	gwFinished := GameWeek{IsCurrent: true, Finished: true}
	if gwFinished.IsActive() {
		t.Errorf("IsActive() = true, want false for finished gameweek")
	}

	gwNext := GameWeek{IsNext: true}
	if !gwNext.IsUpcoming() {
		t.Errorf("IsUpcoming() = false, want true for is_next")
	}

	// DeadlinePassed
	pastDeadline := GameWeek{DeadlineTime: time.Now().Add(-1 * time.Hour)}
	if !pastDeadline.DeadlinePassed() {
		t.Errorf("DeadlinePassed() = false, want true for past deadline")
	}

	futureDeadline := GameWeek{DeadlineTime: time.Now().Add(24 * time.Hour)}
	if futureDeadline.DeadlinePassed() {
		t.Errorf("DeadlinePassed() = true, want false for future deadline")
	}

	zeroDeadline := GameWeek{}
	if zeroDeadline.DeadlinePassed() {
		t.Errorf("DeadlinePassed() = true, want false for zero deadline")
	}
}

func TestManagerTeamAndPickHelpers(t *testing.T) {
	picks := []Pick{
		{Element: 10, Position: 1, IsCaptain: false},
		{Element: 20, Position: 2, IsCaptain: true},
		{Element: 30, Position: 3, IsViceCaptain: true},
		{Element: 40, Position: 11},
		{Element: 50, Position: 12}, // bench
		{Element: 60, Position: 15}, // bench
	}

	if !picks[0].IsStarter() || picks[0].IsBench() {
		t.Errorf("pick[0] IsStarter/IsBench = %v/%v, want true/false", picks[0].IsStarter(), picks[0].IsBench())
	}
	if picks[4].IsStarter() || !picks[4].IsBench() {
		t.Errorf("pick[4] IsStarter/IsBench = %v/%v, want false/true", picks[4].IsStarter(), picks[4].IsBench())
	}

	invalidPick := Pick{Element: 99, Position: 16}
	if invalidPick.IsStarter() || invalidPick.IsBench() {
		t.Errorf("invalidPick (position 16) IsStarter/IsBench = %v/%v, want false/false", invalidPick.IsStarter(), invalidPick.IsBench())
	}
	zeroPick := Pick{Element: 98, Position: 0}
	if zeroPick.IsStarter() || zeroPick.IsBench() {
		t.Errorf("zeroPick (position 0) IsStarter/IsBench = %v/%v, want false/false", zeroPick.IsStarter(), zeroPick.IsBench())
	}

	team := ManagerTeam{Picks: picks}
	capt := team.GetCaptain()
	if capt == nil || capt.Element != 20 {
		t.Errorf("GetCaptain() = %+v, want element 20", capt)
	}

	vc := team.GetViceCaptain()
	if vc == nil || vc.Element != 30 {
		t.Errorf("GetViceCaptain() = %+v, want element 30", vc)
	}

	noCaptainTeam := ManagerTeam{Picks: []Pick{{Element: 1}}}
	if noCaptainTeam.GetCaptain() != nil {
		t.Errorf("GetCaptain() should be nil when no captain")
	}
	if noCaptainTeam.GetViceCaptain() != nil {
		t.Errorf("GetViceCaptain() should be nil when no vice-captain")
	}
}

func TestManagerHistoryHelpers(t *testing.T) {
	ce := CurrentEvents{
		Value: 1015, // £101.5m
		Bank:  25,   // £2.5m
	}
	if got := ce.GetTeamValueInMillions(); got != 101.5 {
		t.Errorf("GetTeamValueInMillions() = %v, want 101.5", got)
	}
	if got := ce.GetBankValueInMillions(); got != 2.5 {
		t.Errorf("GetBankValueInMillions() = %v, want 2.5", got)
	}
}
