package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

// SearchOrganizations exposes only public, active organizations that currently
// host a discoverable activity in this city. Organizations have no city_id.
func (s *Store) SearchOrganizations(ctx context.Context, cityID, query string) ([]agentworkspace.Organization, error) {
	rows, err := s.pool.Query(ctx, `SELECT o.id, o.name, o.description, o.verification_status
		FROM organizations o JOIN accounts owner ON owner.id = o.account_id AND owner.status = 'active'
		WHERE o.status = 'active' AND o.visibility = 'public'
		  AND ($2 = '' OR strpos(lower(o.name || ' ' || o.description), lower($2)) > 0)
		  AND EXISTS (SELECT 1 FROM activities a JOIN cities c ON c.id = a.city_id
			WHERE a.organization_id = o.id AND a.city_id = $1
			AND c.publication_status = 'published'
			AND a.publication_status = 'published' AND a.visibility = 'public'
			AND a.cancelled_at IS NULL AND a.ends_at > now()
			AND (a.expires_at IS NULL OR a.expires_at > now()))
		ORDER BY o.name, o.id LIMIT 30`, cityID, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	organizations := make([]agentworkspace.Organization, 0)
	for rows.Next() {
		var item agentworkspace.Organization
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.VerificationStatus); err != nil {
			return nil, err
		}
		organizations = append(organizations, item)
	}
	return organizations, rows.Err()
}

func (s *Store) Search(ctx context.Context, cityID, viewerID string, terms []string) (agentworkspace.Results, error) {
	result := agentworkspace.Results{
		CityID: cityID, Mode: "rules",
		Activities: []foundation.Activity{}, People: []agentworkspace.Person{},
		Groups: []agentworkspace.Group{}, Places: []foundation.Place{},
	}
	if len(terms) == 0 {
		return result, nil
	}
	var viewer any
	if viewerID != "" {
		viewer = viewerID
	}
	activities, err := s.pool.Query(ctx, `SELECT `+activityColumns+publishedActivityFrom+`
        AND a.city_id = $1 AND a.cancelled_at IS NULL AND a.ends_at > now()
        AND (a.expires_at IS NULL OR a.expires_at > now())
        AND EXISTS (SELECT 1 FROM unnest($3::text[]) term
            WHERE strpos(lower(a.title || ' ' || a.summary || ' ' || `+projectedActivityLabelSQL("$2", "a.host_label")+`), term) > 0)
        ORDER BY a.starts_at, a.id LIMIT 30`, cityID, viewer, terms)
	if err != nil {
		return result, err
	}
	for activities.Next() {
		activity, err := scanActivity(activities)
		if err != nil {
			activities.Close()
			return result, err
		}
		result.Activities = append(result.Activities, activity)
	}
	err = activities.Err()
	activities.Close()
	if err != nil {
		return result, err
	}

	people, err := s.pool.Query(ctx, `SELECT DISTINCT ON (i.owner_account_id)
            i.owner_account_id,
            CASE WHEN birdtie_agent_profile_field_allowed(a.id,$2::uuid,'displayName')
                THEN COALESCE(NULLIF(p.display_name,''),NULLIF(a.handle,''),'Birdtie 成员')
                ELSE 'Birdtie 成员' END, i.topic, i.coarse_area_label,
            COALESCE(i.public_map_zone, ''),
            CASE WHEN i.public_map_zone IS NOT NULL AND c.map_center_latitude IS NOT NULL THEN
                GREATEST(-89.0, LEAST(89.0, c.map_center_latitude +
                    CASE i.public_map_zone
                        WHEN 'north' THEN 0.04 WHEN 'south' THEN -0.04
                        ELSE 0 END)) END,
            CASE WHEN i.public_map_zone IS NOT NULL AND c.map_center_longitude IS NOT NULL THEN
                GREATEST(-179.0, LEAST(179.0, c.map_center_longitude +
                    CASE i.public_map_zone
                        WHEN 'east' THEN 0.06 WHEN 'west' THEN -0.06
                        ELSE 0 END)) END
        FROM intents i
        JOIN cities c ON c.id = i.city_id AND c.publication_status = 'published'
        JOIN accounts a ON a.id = i.owner_account_id AND a.status = 'active'
        JOIN user_profiles p ON p.account_id = a.id AND p.visibility = 'public'
        WHERE i.city_id = $1 AND i.state = 'active' AND i.audience = 'public'
          AND i.owner_confirmed_at IS NOT NULL AND i.expires_at > now()
          AND i.available_from <= now() AND i.available_until > now()
          AND NOT EXISTS (SELECT 1 FROM account_blocks b WHERE $2::uuid IS NOT NULL
              AND ((b.blocker_account_id = i.owner_account_id AND b.blocked_account_id = $2)
                OR (b.blocker_account_id = $2 AND b.blocked_account_id = i.owner_account_id)))
          AND EXISTS (SELECT 1 FROM unnest($3::text[]) term
              WHERE strpos(lower(i.topic || ' ' || i.details || ' ' ||
                  CASE WHEN birdtie_agent_profile_field_allowed(a.id,$2::uuid,'displayName')
                      THEN p.display_name ELSE '' END), term) > 0)
        ORDER BY i.owner_account_id, i.updated_at DESC, i.id DESC LIMIT 20`, cityID, viewer, terms)
	if err != nil {
		return result, err
	}
	for people.Next() {
		var person agentworkspace.Person
		if err := people.Scan(&person.AccountID, &person.DisplayName, &person.Topic,
			&person.AreaLabel, &person.PublicMapZone, &person.MapLatitude,
			&person.MapLongitude); err != nil {
			people.Close()
			return result, err
		}
		result.People = append(result.People, person)
	}
	err = people.Err()
	people.Close()
	if err != nil {
		return result, err
	}

	groups, err := s.pool.Query(ctx, `SELECT g.id, g.name, g.summary,
            CASE WHEN p.location_precision = 'point' AND p.coordinate_system = 'wgs84' THEN p.latitude END,
            CASE WHEN p.location_precision = 'point' AND p.coordinate_system = 'wgs84' THEN p.longitude END,
            g.source_label, g.source_ref,
            CASE WHEN EXISTS (SELECT 1 FROM audit_events origin_event
                WHERE origin_event.resource_type = 'community'
                  AND origin_event.resource_id = g.id::text
                  AND origin_event.decision = 'allowed'
                  AND origin_event.actor_account_id IS NOT NULL
                  AND ((origin_event.action = 'community_create'
                        AND origin_event.purpose = 'community_social_layer')
                    OR (origin_event.action = 'publish'
                        AND origin_event.purpose = 'owner_confirmed')))
                THEN '社区维护者' ELSE g.maintainer_label END,
            g.updated_at, g.verified_at, g.expires_at
        FROM communities g
        JOIN accounts a ON a.id = g.owner_account_id AND a.status = 'active'
        JOIN cities c ON c.id = g.city_id AND c.publication_status = 'published'
        LEFT JOIN places p ON p.id = g.place_id AND p.publication_status = 'published'
          AND (p.expires_at IS NULL OR p.expires_at > now())
        WHERE g.city_id = $1 AND g.visibility = 'public'
          AND g.publication_status = 'published' AND g.owner_confirmed_at IS NOT NULL
          AND (g.expires_at IS NULL OR g.expires_at > now())
          AND NOT EXISTS (SELECT 1 FROM account_blocks b WHERE $2::uuid IS NOT NULL
              AND ((b.blocker_account_id = g.owner_account_id AND b.blocked_account_id = $2)
                OR (b.blocker_account_id = $2 AND b.blocked_account_id = g.owner_account_id)))
          AND EXISTS (SELECT 1 FROM unnest($3::text[]) term
              WHERE strpos(lower(g.name || ' ' || g.summary), term) > 0)
        ORDER BY g.name, g.id LIMIT 20`, cityID, viewer, terms)
	if err != nil {
		return result, err
	}
	for groups.Next() {
		var group agentworkspace.Group
		group.EntityType = "community" // Actual FROM communities, never label inference.
		var latitude, longitude *float64
		if err := groups.Scan(&group.ID, &group.Name, &group.Summary, &latitude, &longitude,
			&group.Source.Label, &group.Source.Reference, &group.Source.Maintainer,
			&group.Source.UpdatedAt, &group.Source.VerifiedAt, &group.Source.ExpiresAt); err != nil {
			groups.Close()
			return result, err
		}
		if latitude != nil && longitude != nil {
			group.Location = &foundation.Location{
				CoordinateSystem: "wgs84", Precision: "point", Latitude: latitude, Longitude: longitude,
			}
		}
		group.Source.SetFreshness(time.Now().UTC())
		result.Groups = append(result.Groups, group)
	}
	err = groups.Err()
	groups.Close()
	if err != nil {
		return result, err
	}

	places, err := s.pool.Query(ctx, `SELECT `+placeColumns+`
        FROM places p JOIN cities c ON c.id = p.city_id AND c.publication_status = 'published'
        WHERE p.city_id = $1 AND p.publication_status = 'published'
          AND (p.expires_at IS NULL OR p.expires_at > now())
          AND EXISTS (SELECT 1 FROM unnest($2::text[]) term
              WHERE strpos(lower(p.name || ' ' || p.summary || ' ' || p.category_code), term) > 0)
        ORDER BY p.name, p.id LIMIT 30`, cityID, terms)
	if err != nil {
		return result, err
	}
	for places.Next() {
		place, err := scanPlace(places)
		if err != nil {
			places.Close()
			return result, err
		}
		result.Places = append(result.Places, place)
	}
	err = places.Err()
	places.Close()
	return result, err
}

const agentTaskColumns = `id, principal_type, owner_account_id, acting_user_account_id,
    query, intent, status, COALESCE(city_context_id,''), context_type, context_id,
    filters, conversation, created_at, updated_at`

func (s *Store) HasActiveAgent(ctx context.Context, principalType, accountID string) (bool, error) {
	var active bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (
        SELECT 1 FROM agents ag JOIN accounts a ON a.id=ag.principal_account_id
        WHERE ag.principal_account_id=$2 AND ag.status='active' AND a.status='active'
          AND (($1='person' AND ag.agent_type='personal' AND a.account_type='person') OR
               ($1='organization' AND ag.agent_type='organization' AND a.account_type='organization'))
    )`, principalType, accountID).Scan(&active)
	return active, err
}

func (s *Store) SaveTask(ctx context.Context, task agentworkspace.Task) (agentworkspace.Task, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return agentworkspace.Task{}, err
	}
	defer tx.Rollback(ctx)
	stored, err := s.saveTaskInTx(ctx, tx, task)
	if err != nil {
		return agentworkspace.Task{}, err
	}
	return stored, tx.Commit(ctx)
}
func (s *Store) saveTaskInTx(ctx context.Context, tx pgx.Tx, task agentworkspace.Task) (agentworkspace.Task, error) {
	if task.ContextID == "" {
		if task.CityID == "" || task.ContextType != "" {
			return agentworkspace.Task{}, errors.New("Agent task requires city or typed context")
		}
	} else {
		ref, err := task.ContextRef()
		if err != nil || (task.CityID == "" && string(ref.Type) == "CITY") ||
			(task.CityID != "" && string(ref.Type) != "CITY") {
			return agentworkspace.Task{}, errors.New("Agent task context/city mismatch")
		}
	}
	filters, err := json.Marshal(task.Filters)
	if err != nil {
		return agentworkspace.Task{}, err
	}
	conversation, err := json.Marshal(task.Conversation)
	if err != nil {
		return agentworkspace.Task{}, err
	}
	var actingUser any
	if task.ActingUserID != "" {
		actingUser = task.ActingUserID
	}
	row := tx.QueryRow(ctx, `INSERT INTO agent_tasks
        (principal_type, owner_account_id, acting_user_account_id, city_id, city_context_id,
         query, intent, status, filters, conversation, context_type, context_id)
        VALUES ($1, $2, $3, NULLIF($4,''), NULLIF($4,''), $5, $6, $7, $8, $9,
                COALESCE(NULLIF($10,''),'CITY'), NULLIF($11,'')::uuid)
        RETURNING `+agentTaskColumns,
		task.PrincipalType, task.PrincipalID, actingUser, task.CityID, task.Query,
		task.Intent, task.Status, filters, conversation, task.ContextType, task.ContextID)
	stored, err := scanAgentTask(row)
	if err != nil {
		return agentworkspace.Task{}, err
	}
	actor := stored.ActingUserID
	if actor == "" {
		actor = stored.PrincipalID
	}
	if err = insertDomainAudit(ctx, tx, actor, "create", "agent_task", stored.ID, "human_agent_task", nil); err != nil {
		return agentworkspace.Task{}, err
	}
	return stored, nil
}

func (s *Store) UpdateTask(ctx context.Context, task agentworkspace.Task) (agentworkspace.Task, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return agentworkspace.Task{}, err
	}
	defer tx.Rollback(ctx)
	updated, err := s.updateTaskInTx(ctx, tx, task)
	if err != nil {
		return agentworkspace.Task{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return agentworkspace.Task{}, err
	}
	return updated, nil
}
func (s *Store) updateTaskInTx(ctx context.Context, tx pgx.Tx, task agentworkspace.Task) (agentworkspace.Task, error) {
	return s.updateTaskInTxWithAuditActor(ctx, tx, task, "")
}

// Organization continuation keeps the original Task creator while attributing
// each committed update to the current server-verified invocation actor.
// An empty override preserves the existing Personal and legacy writer contract.
func (s *Store) updateTaskInTxWithAuditActor(ctx context.Context, tx pgx.Tx, task agentworkspace.Task, auditActor string) (agentworkspace.Task, error) {
	filters, err := json.Marshal(task.Filters)
	if err != nil {
		return agentworkspace.Task{}, err
	}
	conversation, err := json.Marshal(task.Conversation)
	if err != nil {
		return agentworkspace.Task{}, err
	}
	previous, err := scanAgentTask(tx.QueryRow(ctx, `SELECT `+agentTaskColumns+`
        FROM agent_tasks WHERE id=$1 AND owner_account_id=$2 FOR UPDATE`, task.ID, task.PrincipalID))
	if err != nil {
		return agentworkspace.Task{}, err
	}
	// An exact native retry preserves the original row/event identity. In
	// particular, retrying a terminal response cannot create a second notice.
	var changed bool
	if err = tx.QueryRow(ctx, `SELECT intent IS DISTINCT FROM $3 OR status IS DISTINCT FROM $4
        OR filters IS DISTINCT FROM $5::jsonb OR conversation IS DISTINCT FROM $6::jsonb
        FROM agent_tasks WHERE id=$1 AND owner_account_id=$2`, task.ID, task.PrincipalID,
		task.Intent, task.Status, filters, conversation).Scan(&changed); err != nil {
		return agentworkspace.Task{}, err
	}
	if !changed {
		return previous, nil
	}
	row := tx.QueryRow(ctx, `UPDATE agent_tasks SET intent=$3, status=$4, filters=$5,
        conversation=$6, updated_at=now()
        WHERE id=$1 AND owner_account_id=$2
        RETURNING `+agentTaskColumns, task.ID, task.PrincipalID, task.Intent,
		task.Status, filters, conversation)
	updated, err := scanAgentTask(row)
	if err != nil {
		return agentworkspace.Task{}, err
	}
	actor := auditActor
	if actor == "" {
		actor = updated.ActingUserID
	}
	if actor == "" {
		actor = updated.PrincipalID
	}
	if err = insertDomainAudit(ctx, tx, actor, "update", "agent_task", updated.ID, "human_agent_task", nil); err != nil {
		return agentworkspace.Task{}, err
	}
	if updated.PrincipalType == "person" && (updated.Status == "COMPLETED" || updated.Status == "FAILED") {
		kind := agentnotification.KindAgentTaskCompleted
		if updated.Status == "FAILED" {
			kind = agentnotification.KindAgentTaskFailed
		}
		if _, err = routeNativeNotification(ctx, tx, kind, updated.ID, updated.PrincipalID); err != nil {
			return agentworkspace.Task{}, err
		}
	}
	return updated, nil
}

func (s *Store) ListTasks(ctx context.Context, ownerID string) ([]agentworkspace.Task, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+agentTaskColumns+` FROM agent_tasks
        WHERE owner_account_id = $1 ORDER BY updated_at DESC, id DESC LIMIT 50`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := make([]agentworkspace.Task, 0)
	for rows.Next() {
		task, err := scanAgentTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (s *Store) GetTask(ctx context.Context, ownerID, id string) (agentworkspace.Task, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+agentTaskColumns+` FROM agent_tasks
        WHERE id = $1 AND owner_account_id = $2`, id, ownerID)
	return scanAgentTask(row)
}

// ValidateHumanAgentTask runs after response encoding and commercial reads.
// The native Session, active Personal Agent and exact original task are checked
// after relation waits; a successful Inbox read is not a reusable permission.
func (s *Store) ValidateHumanAgentTask(ctx context.Context, digest [32]byte, actor identity.Actor, expected agentworkspace.Task) error {
	if actor.AccountType != "person" || expected.PrincipalID != actor.ID {
		return identity.ErrUnauthorized
	}
	tx, err := s.beginHumanInboxTx(ctx, digest, actor)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	current, err := scanAgentTask(tx.QueryRow(ctx, `SELECT `+agentTaskColumns+` FROM agent_tasks
 WHERE id=$1 AND owner_account_id=$2 AND principal_type='person'
 AND EXISTS(SELECT 1 FROM agents a WHERE a.principal_account_id=$2 AND a.agent_type='personal' AND a.status='active')
 FOR SHARE`, expected.ID, actor.ID))
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(agentworkspace.SanitizeTaskForResponse(current), expected) {
		return agentworkspace.ErrNotFound
	}
	if err = s.lockHumanMomentSession(ctx, tx, digest, actor.ID); err != nil {
		return err
	}
	// Relation locks were acquired before any row wait. This final statement has
	// no new row lock: introducing Agent-after-Session locks would invert the
	// existing Account/Agent/Memory/Session writers' order. Task and Session are
	// already held; recheck active Agent and absolute Session time together after
	// the last Session wait, with no further authentication/source read afterward.
	var sessionCurrent, agentCurrent bool
	err = tx.QueryRow(ctx, `WITH n AS MATERIALIZED(SELECT clock_timestamp() at)
 SELECT EXISTS(SELECT 1 FROM sessions s CROSS JOIN n WHERE s.token_sha256=$1 AND s.account_id=$2
 AND s.revoked_at IS NULL AND s.expires_at>n.at AND s.idle_expires_at>n.at
 AND ($3::boolean OR s.authentication_method<>'dev_phone')),
 EXISTS(SELECT 1 FROM agents ag WHERE ag.principal_account_id=$2 AND ag.agent_type='personal' AND ag.status='active')`,
		digest[:], actor.ID, s.devPhoneEnabled).Scan(&sessionCurrent, &agentCurrent)
	if err != nil {
		return err
	}
	if !sessionCurrent {
		return identity.ErrUnauthorized
	}
	if !agentCurrent {
		return agentworkspace.ErrNotFound
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return ctx.Err()
}

func scanAgentTask(row scanner) (agentworkspace.Task, error) {
	var task agentworkspace.Task
	var actingUser *string
	var filters, conversation []byte
	err := row.Scan(&task.ID, &task.PrincipalType, &task.PrincipalID, &actingUser,
		&task.Query, &task.Intent, &task.Status, &task.CityID,
		&task.ContextType, &task.ContextID, &filters,
		&conversation, &task.CreatedAt, &task.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return agentworkspace.Task{}, agentworkspace.ErrNotFound
	}
	if err != nil {
		return agentworkspace.Task{}, err
	}
	if actingUser != nil {
		task.ActingUserID = *actingUser
	}
	if err := json.Unmarshal(filters, &task.Filters); err != nil {
		return agentworkspace.Task{}, err
	}
	if err := json.Unmarshal(conversation, &task.Conversation); err != nil {
		return agentworkspace.Task{}, err
	}
	if task.Filters == nil {
		task.Filters = map[string]string{}
	}
	if task.Conversation == nil {
		task.Conversation = []agentworkspace.Message{}
	}
	return task, err
}
