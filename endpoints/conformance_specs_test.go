package endpoints_test

// Allowlists codify which FPL API keys each model deliberately does not map.
//
// The conformance rules (see internal/conformance) fail on any payload key
// without a model field, so every key the API returns but we choose not to
// expose must be listed here. Each entry is an explicit, reviewable decision;
// when the API adds keys, conformance failures tell you exactly what to add
// where — the model, if we want the data, or this file, if we don't.
//
// Last reconciled against the live API on 2026-08-22 (2026/27, GW1 in play).

// bootstrapAllowlist covers top-level /bootstrap-static/ keys outside the
// endpoints.Response wrapper.
var bootstrapAllowlist = []string{
	"chips",
	"contests", // The live API currently returns an empty array.
	"element_stats",
	"element_types",
	"game_config",
	"phases",
	"total_players",
}

// eventLiveAllowlist: every key in /event/{id}/live/ payloads is mapped onto
// the EventLive model; declared so a future API addition fails loudly here
// instead of silently zero-valuing.
var eventLiveAllowlist = []string{}

// teamAllowlist: "link_url" was added by the API and is not yet mapped.
var teamAllowlist = []string{
	"link_url",
}

// playerAllowlist: statistics, per-90 variants, rank variants, and metadata
// the Player model does not currently expose. Several of these (bonus, bps,
// saves, tackles, own_goals, penalties_*) look like mapping backlog.
// price_change_* keys are the API's internal price-rise scheduling machinery;
// "price_change_percent" was already unmapped for the same reason.
var playerAllowlist = []string{
	"birth_date",
	"bonus",
	"bps",
	"can_select",
	"can_transact",
	"chance_of_playing_this_round",
	"clean_sheets_per_90",
	"clearances_blocks_interceptions",
	"corners_and_indirect_freekicks_order",
	"corners_and_indirect_freekicks_text",
	"creativity_rank",
	"creativity_rank_type",
	"defensive_contribution",
	"defensive_contribution_per_90",
	"direct_freekicks_order",
	"direct_freekicks_text",
	"expected_assists_per_90",
	"expected_goal_involvements_per_90",
	"expected_goals_conceded_per_90",
	"expected_goals_per_90",
	"goals_conceded",
	"goals_conceded_per_90",
	"has_temporary_code",
	"ict_index_rank",
	"ict_index_rank_type",
	"influence_rank",
	"influence_rank_type",
	"known_name",
	"opta_code",
	"own_goals",
	"penalties_missed",
	"penalties_order",
	"penalties_saved",
	"penalties_text",
	"photo",
	"price_change_calibrating",
	"price_change_hourly_rate",
	"price_change_locked_until",
	"price_change_projections",
	"price_change_percent",
	"recoveries",
	"region",
	"removed",
	"saves",
	"saves_per_90",
	"scout_news_link",
	"scout_risks",
	"starts_per_90",
	"tackles",
	"team_join_date",
	"threat_rank",
	"threat_rank_type",
}

// gameWeekAllowlist covers event-management flags not exposed on GameWeek.
var gameWeekAllowlist = []string{
	"can_enter",
	"can_manage",
	"overrides",
	"released",
}

// gameSettingsAllowlist: newer game-setting keys not yet mapped.
var gameSettingsAllowlist = []string{
	"element_sell_at_purchase_price",
	"squad_special_max",
	"squad_special_min",
	"underdog_differential",
}

// pastHistoryStatsAllowlist: nothing is currently unmapped on PastHistoryStats;
// kept as an explicit empty decision so future API additions surface here.
var pastHistoryStatsAllowlist = []string{}

// managerAllowlist covers /entry/ keys not exposed on Manager.
var managerAllowlist = []string{
	"club_badge_src",
	"leagues",
}
