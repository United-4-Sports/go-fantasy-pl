package endpoints

import (
	"context"
	"fmt"

	"github.com/AbdoAnss/go-fantasy-pl/api"
	"github.com/AbdoAnss/go-fantasy-pl/models"
)

// TeamService provides access to team-related data from the FPL API.
// Teams include attributes such as ID, name, and strength ratings for both
// home and away matches.
type TeamService struct {
	client           api.Client
	bootstrapService *BootstrapService
}

// NewTeamService creates a new instance of the TeamService.
func NewTeamService(client api.Client, bootstrap *BootstrapService) *TeamService {
	return &TeamService{
		client:           client,
		bootstrapService: bootstrap,
	}
}

// GetAllTeams returns a list of all Premier League teams participating in the FPL season.
// This is a convenience wrapper around BootstrapService.GetTeams.
func (ts *TeamService) GetAllTeams() ([]models.Team, error) {
	return ts.GetAllTeamsWithContext(context.Background())
}

// GetAllTeamsWithContext returns a list of all Premier League teams with context.
func (ts *TeamService) GetAllTeamsWithContext(ctx context.Context) ([]models.Team, error) {
	return ts.bootstrapService.GetTeamsWithContext(ctx)
}

// GetTeam returns a single team by its unique FPL ID.
func (ts *TeamService) GetTeam(id int) (*models.Team, error) {
	return ts.GetTeamWithContext(context.Background(), id)
}

// GetTeamWithContext returns a single team by its unique FPL ID with context.
func (ts *TeamService) GetTeamWithContext(ctx context.Context, id int) (*models.Team, error) {
	teams, err := ts.GetAllTeamsWithContext(ctx)
	if err != nil {
		return nil, err
	}

	for _, t := range teams {
		if t.ID == id {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("team with ID %d not found", id)
}
