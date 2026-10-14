package postgres

import (
	"context"
	"time"

	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/jackc/pgx/v5"
)

// A finite local Context Builder extension. Native owner/session/Agent/metadata
// locks are held by its caller. No human-review seal or model grant is reused.
func contextBuilderLocalFoodTx(ctx context.Context, tx pgx.Tx, binding agentPrivateBinding, task agentworkspace.Task, now time.Time) ([]acb.LocalFoodPreference, error) {
	question, ok := agentworkspace.ParseLocalPlaceQuestion(task.Filters["currentQuery"])
	if !ok || !question.Recommendation || task.PrincipalID != binding.accountID || task.ActingUserID != binding.accountID || task.ContextType != "CITY" {
		return nil, acb.ErrDenied
	}
	rows, e := tx.Query(ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories WHERE agent_id=$1 AND owner_id=$2 AND owner_type='PERSON'
 AND memory_type='PREFERENCE' AND memory_key IN ('now.preference.food.seafood','now.preference.food.spicy')
 AND source_type='EXPLICIT' AND visibility='PRIVATE' AND status='ACTIVE' AND valid_from<=$3 AND valid_until>$3 ORDER BY memory_key,id FOR SHARE`, binding.agentID, binding.accountID, now)
	if e != nil {
		return nil, acb.ErrUnavailable
	}
	memories := []struct {
		projection acb.LocalFoodPreference
		value      agentworkspace.NowPreferenceValue
	}{}
	for rows.Next() {
		m, err := scanAgentMemory(rows)
		if err != nil {
			rows.Close()
			return nil, acb.ErrUnavailable
		}
		v, err := agentworkspace.ParseNowPreferenceMemory(m, now)
		if err != nil {
			continue
		}
		memories = append(memories, struct {
			projection acb.LocalFoodPreference
			value      agentworkspace.NowPreferenceValue
		}{acb.LocalFoodPreference{MemoryID: m.ID, Version: m.Version, Topic: v.Topic, Value: v.Value}, v})
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, acb.ErrUnavailable
	}
	out := []acb.LocalFoodPreference{}
	seen := map[string]bool{}
	for _, m := range memories {
		if seen[m.projection.Topic] {
			return nil, acb.ErrDenied
		}
		if e = validateNowPreferenceSourcesTx(ctx, tx, binding, m.value); e != nil {
			return nil, acb.ErrDenied
		}
		seen[m.projection.Topic] = true
		out = append(out, m.projection)
	}
	return out, nil
}
