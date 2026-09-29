package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/jackc/pgx/v5"
)

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
            WHERE strpos(lower(a.title || ' ' || a.summary || ' ' || a.host_label), term) > 0)
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
            i.owner_account_id, p.display_name, i.topic, i.coarse_area_label,
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
              WHERE strpos(lower(i.topic || ' ' || i.details || ' ' || p.display_name), term) > 0)
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
            g.source_label, g.source_ref, g.maintainer_label,
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
    query, intent, status, city_context_id, filters, conversation, created_at, updated_at`

func (s *Store) SaveTask(ctx context.Context, task agentworkspace.Task) (agentworkspace.Task, error) {
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
	row := s.pool.QueryRow(ctx, `INSERT INTO agent_tasks
        (principal_type, owner_account_id, acting_user_account_id, city_id, city_context_id,
         query, intent, status, filters, conversation)
        VALUES ($1, $2, $3, $4, $4, $5, $6, $7, $8, $9) RETURNING `+agentTaskColumns,
		task.PrincipalType, task.PrincipalID, actingUser, task.CityID, task.Query,
		task.Intent, task.Status, filters, conversation)
	return scanAgentTask(row)
}

func (s *Store) UpdateTask(ctx context.Context, task agentworkspace.Task) (agentworkspace.Task, error) {
	filters, err := json.Marshal(task.Filters)
	if err != nil {
		return agentworkspace.Task{}, err
	}
	conversation, err := json.Marshal(task.Conversation)
	if err != nil {
		return agentworkspace.Task{}, err
	}
	row := s.pool.QueryRow(ctx, `UPDATE agent_tasks SET intent=$3, status=$4, filters=$5,
        conversation=$6, updated_at=now()
        WHERE id=$1 AND owner_account_id=$2
        RETURNING `+agentTaskColumns, task.ID, task.PrincipalID, task.Intent,
		task.Status, filters, conversation)
	return scanAgentTask(row)
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

func scanAgentTask(row scanner) (agentworkspace.Task, error) {
	var task agentworkspace.Task
	var actingUser *string
	var filters, conversation []byte
	err := row.Scan(&task.ID, &task.PrincipalType, &task.PrincipalID, &actingUser,
		&task.Query, &task.Intent, &task.Status, &task.CityID, &filters,
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
