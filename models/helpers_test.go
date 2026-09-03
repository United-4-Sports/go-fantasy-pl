package models

import (
	"testing"
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
			available: true,
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
			available: false,
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
}
