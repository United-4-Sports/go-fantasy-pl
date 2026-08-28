package main

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/AbdoAnss/go-fantasy-pl/client"
	"github.com/AbdoAnss/go-fantasy-pl/models"
)

const separator = "----------------------------------------"

func main() {
	// Step 1: Initialize the FPL client
	fpl, err := client.NewClient()
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Step 2: Fetch core datasets concurrently
	teamsCh := fpl.Teams.GetAllTeamsAsync(ctx)
	playersCh := fpl.Players.GetAllPlayersAsync(ctx)
	fixturesCh := fpl.Fixtures.GetAllFixturesAsync(ctx)

	teamsResult := <-teamsCh
	playersResult := <-playersCh
	fixturesResult := <-fixturesCh

	if teamsResult.Err != nil {
		log.Fatalf("Failed to get teams: %v", teamsResult.Err)
	}
	if playersResult.Err != nil {
		log.Fatalf("Failed to get players: %v", playersResult.Err)
	}
	if fixturesResult.Err != nil {
		log.Fatalf("Failed to get fixtures: %v", fixturesResult.Err)
	}

	teams := teamsResult.Value
	teamMap := make(map[int]models.Team)
	for _, team := range teams {
		teamMap[team.ID] = team
	}

	// Step 3: Analyze team strengths
	fmt.Println("Top 5 Teams by Overall Strength:")
	sortedTeams := make([]models.Team, len(teams))
	copy(sortedTeams, teams)
	sort.Slice(sortedTeams, func(i, j int) bool {
		return sortedTeams[i].Strength > sortedTeams[j].Strength
	})
	for i, team := range sortedTeams[:min(5, len(sortedTeams))] {
		fmt.Printf("%d. %s (Strength: %d)\n", i+1, team.GetFullName(), team.Strength)
	}
	fmt.Println(separator)

	// Step 4: Use the concurrently fetched players
	players := playersResult.Value

	// Sort players by total points
	sort.Slice(players, func(i, j int) bool {
		return players[i].TotalPoints > players[j].TotalPoints
	})

	// Step 5: Display top 5 players
	fmt.Println("Top 5 Players by Total Points:")
	for i, player := range players[:min(5, len(players))] {
		team := teamMap[player.Team]
		fmt.Printf("%d. %s (%s) - £%.1fm - %d points\n",
			i+1,
			player.GetDisplayName(),
			team.GetShortName(),
			player.GetPriceInPounds(),
			player.TotalPoints)
	}
	fmt.Println(separator)

	// Step 6: Use the concurrently fetched fixtures
	fixtures := fixturesResult.Value

	// Filter for fixtures with a known kickoff and sort them chronologically.
	var scheduledFixtures []models.Fixture
	for _, fix := range fixtures {
		if fix.KickoffTime != nil {
			scheduledFixtures = append(scheduledFixtures, fix)
		}
	}
	sort.Slice(scheduledFixtures, func(i, j int) bool {
		return scheduledFixtures[i].KickoffTime.Before(*scheduledFixtures[j].KickoffTime)
	})

	// Step 7: Display the next 5 scheduled fixtures with team strengths.
	fmt.Println("Next 5 Scheduled Fixtures (with Team Strengths):")
	for i, fix := range scheduledFixtures[:min(5, len(scheduledFixtures))] {
		homeTeam := teamMap[fix.TeamH]
		awayTeam := teamMap[fix.TeamA]
		gameweek := 0
		if fix.Event != nil {
			gameweek = *fix.Event
		}
		fmt.Printf("%d. %s (%d) vs %s (%d) - Gameweek %d\n",
			i+1,
			homeTeam.GetShortName(), homeTeam.Strength,
			awayTeam.GetShortName(), awayTeam.Strength,
			gameweek)
	}
	fmt.Println(separator)

	// Step 8: Get detailed history for a top player
	topPlayer := players[0]
	historyResult := <-fpl.Players.GetPlayerHistoryAsync(ctx, topPlayer.ID)
	if historyResult.Err != nil {
		log.Fatalf("Failed to get player history: %v", historyResult.Err)
	}
	history := historyResult.Value

	fmt.Printf("Recent Performance - %s:\n", topPlayer.GetDisplayName())
	sort.Slice(history.History, func(i, j int) bool {
		return history.History[i].Round > history.History[j].Round
	})

	for i, game := range history.History[:min(3, len(history.History))] {
		fmt.Printf("%d. GW%d: %d pts (Goals: %d, Assists: %d)\n",
			i+1,
			game.Round,
			game.TotalPoints,
			game.GoalsScored,
			game.Assists)
	}
}
