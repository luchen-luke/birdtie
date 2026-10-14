package main

import (
	"errors"
	aa "github.com/birdtie/birdtie/apps/api/internal/agentaction"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
)

const agentActivityJoinEnabledKey = "BIRDTIE_AGENT_ACTIVITY_JOIN_ENABLED"

// This trusted startup selection never replaces the independent Enrichment
// ticket, current native source or the owner's exact approved action version.
func loadAgentActivityJoinStartup(lookup func(string) (string, bool), store *postgres.Store, flags *agentfeature.Controller) (aa.ActivityJoinHumanGateway, error) {
	if lookup == nil {
		return nil, errors.New("invalid Agent Activity JOIN configuration")
	}
	enabled := false
	if value, present := lookup(agentActivityJoinEnabledKey); present && value != "" {
		switch value {
		case "false":
		case "true":
			enabled = true
		default:
			return nil, errors.New("invalid Agent Activity JOIN configuration")
		}
	}
	return postgres.NewAgentActivityJoin(store, flags, enabled), nil
}
