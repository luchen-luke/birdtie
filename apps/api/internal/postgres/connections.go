package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	"time"

	mp "github.com/birdtie/birdtie/apps/api/internal/agentmessagepolicy"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (s *Store) CreateRequest(ctx context.Context, senderID, recipientID, cityID, note string) (connection.Request, error) {
	return s.createRequest(ctx, senderID, recipientID, cityID, note, nil, nil)
}
func (s *Store) CreateRequestBound(ctx context.Context, a ea.Access, recipientID, cityID, note string, b ea.BoundCondition) (connection.Request, error) {
	if b.Kind != ea.Connect || b.Operation != "REQUEST_CONVERSATION" {
		return connection.Request{}, ea.ErrInvalid
	}
	var e error
	ctx, e = s.messageBindAccess(ctx, a)
	if e != nil {
		return connection.Request{}, e
	}
	return s.createRequest(ctx, a.Actor.ID, recipientID, cityID, note, &a, &b)
}
func (s *Store) createRequest(ctx context.Context, senderID, recipientID, cityID, note string, access *ea.Access, condition *ea.BoundCondition) (connection.Request, error) {
	if senderID == recipientID {
		return connection.Request{}, connection.ErrForbidden
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return connection.Request{}, err
	}
	defer tx.Rollback(ctx)
	routeFence, err := startMessageRoute(ctx, tx, senderID, recipientID)
	if err != nil {
		return connection.Request{}, err
	}
	if !messageRequestAllowed(routeFence) {
		return connection.Request{}, connection.ErrNotFound
	}
	if access != nil {
		if err = s.lockEntityActionWriter(ctx, tx, *access); err != nil {
			return connection.Request{}, err
		}
	}
	var sender string
	err = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id = $1 AND status = 'active' FOR UPDATE`, senderID).Scan(&sender)
	if errors.Is(err, pgx.ErrNoRows) {
		return connection.Request{}, connection.ErrForbidden
	}
	if err != nil {
		return connection.Request{}, err
	}
	var fence entityActionWriteFence
	if access != nil {
		fence, err = s.checkEntityActionWrite(ctx, tx, *access, ea.Ref{Type: "person", ID: recipientID}, *condition)
		if err != nil {
			return connection.Request{}, err
		}
		var currentCity bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cities c JOIN intents i ON i.city_id=c.id WHERE c.id=$1 AND i.owner_account_id=$2 AND i.audience='public' AND i.state='active' AND i.owner_confirmed_at IS NOT NULL AND i.expires_at>statement_timestamp() AND i.available_from<=statement_timestamp() AND i.available_until>statement_timestamp() AND c.publication_status='published' AND (c.expires_at IS NULL OR c.expires_at>statement_timestamp()))`, cityID, recipientID).Scan(&currentCity); err != nil || !currentCity {
			return connection.Request{}, ea.ErrChanged
		}
	}
	_, err = tx.Exec(ctx, `UPDATE connection_requests SET state = 'expired', decided_at = now()
        WHERE state = 'pending' AND expires_at <= now()
          AND (sender_account_id = $1 OR recipient_account_id = $1)`, senderID)
	if err != nil {
		return connection.Request{}, err
	}
	var eligible bool
	err = tx.QueryRow(ctx, `SELECT EXISTS (
        SELECT 1 FROM accounts a
        JOIN user_profiles p ON p.account_id = a.id AND p.visibility = 'public'
        JOIN intents i ON i.owner_account_id = a.id AND i.city_id = $3
          AND i.audience = 'public' AND i.state = 'active'
          AND i.owner_confirmed_at IS NOT NULL AND i.expires_at > now()
          AND i.available_from <= now() AND i.available_until > now()
        JOIN cities c ON c.id = i.city_id AND c.publication_status = 'published'
        WHERE a.id = $2 AND a.status = 'active'
          AND NOT EXISTS (SELECT 1 FROM account_blocks b
              WHERE (b.blocker_account_id = $1 AND b.blocked_account_id = $2)
                 OR (b.blocker_account_id = $2 AND b.blocked_account_id = $1))
          AND NOT EXISTS (SELECT 1 FROM conversations cv
              JOIN connection_requests cr ON cr.id = cv.request_id
              WHERE cr.state = 'accepted'
                AND ((cv.member_a_account_id = $1 AND cv.member_b_account_id = $2)
                  OR (cv.member_a_account_id = $2 AND cv.member_b_account_id = $1)))
    )`, senderID, recipientID, cityID).Scan(&eligible)
	if err != nil {
		return connection.Request{}, err
	}
	if !eligible {
		return connection.Request{}, connection.ErrNotFound
	}
	var pending int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM connection_requests
        WHERE sender_account_id = $1 AND state = 'pending' AND expires_at > now()`, senderID).Scan(&pending)
	if err != nil {
		return connection.Request{}, err
	}
	if pending >= 10 {
		return connection.Request{}, connection.ErrRateLimit
	}
	var req connection.Request
	req.Direction = "outgoing"
	req.OtherAccountID = recipientID
	req.CityID = cityID
	req.Note = note
	req.Scope = "conversation"
	req.State = "pending"
	err = tx.QueryRow(ctx, `INSERT INTO connection_requests
        (sender_account_id, recipient_account_id, city_id, note, expires_at)
        VALUES ($1, $2, $3, $4, now() + interval '7 days')
        RETURNING id, expires_at, created_at`, senderID, recipientID, cityID, note).
		Scan(&req.ID, &req.ExpiresAt, &req.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return connection.Request{}, connection.ErrConflict
		}
		return connection.Request{}, err
	}
	if err = messageRecordRequest(ctx, tx, &req, routeFence); err != nil {
		return connection.Request{}, err
	}
	if messageOrdinaryDelivery(routeFence) {
		_, err = routeNativeNotification(ctx, tx, agentnotification.KindConnectionRequest, req.ID, recipientID)
		if err != nil {
			return connection.Request{}, err
		}
	}
	_, err = auditExec(ctx, tx, `INSERT INTO audit_events
        (actor_account_id, action, resource_type, resource_id, decision, purpose)
        VALUES ($1, 'request', 'connection_request', $2, 'allowed', 'human_contact')`, senderID, req.ID)
	if err != nil {
		return connection.Request{}, err
	}
	if access != nil {
		fence.createdRequestID = req.ID
		var exact bool
		if err = tx.QueryRow(ctx, `SELECT count(*)=1 AND bool_and(id=$3::uuid AND city_id=$4 AND note=$5 AND scope='conversation' AND state='pending') FROM connection_requests WHERE sender_account_id=$1 AND recipient_account_id=$2 AND state='pending'`, senderID, recipientID, req.ID, cityID, note).Scan(&exact); err != nil || !exact {
			return connection.Request{}, ea.ErrChanged
		}
		if err = s.finishEntityActionWrite(ctx, tx, fence); err != nil {
			return connection.Request{}, err
		}
	}
	if err = finishMessageRoute(ctx, tx, routeFence); err != nil {
		return connection.Request{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return connection.Request{}, err
	}
	return req, nil
}

func (s *Store) CreateFriendRequest(ctx context.Context, senderID, recipientID, note string) (connection.Request, error) {
	if senderID == recipientID || len(note) < 1 || len(note) > 280 {
		return connection.Request{}, connection.ErrForbidden
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return connection.Request{}, err
	}
	defer tx.Rollback(ctx)
	routeFence, err := startMessageRoute(ctx, tx, senderID, recipientID)
	if err != nil {
		return connection.Request{}, err
	}
	if !messageRequestAllowed(routeFence) {
		return connection.Request{}, connection.ErrNotFound
	}
	request, err := createFriendRequestWithFenceTx(ctx, tx, senderID, recipientID, note, routeFence)
	if err != nil {
		return connection.Request{}, err
	}
	if err = finishMessageRoute(ctx, tx, routeFence); err != nil {
		return connection.Request{}, err
	}
	return request, tx.Commit(ctx)
}

func (s *Store) CreateFriendRequestBound(ctx context.Context, a ea.Access, recipient, note string, b ea.BoundCondition) (connection.Request, error) {
	if b.Kind != ea.Connect || b.Operation != "REQUEST_FRIEND" {
		return connection.Request{}, ea.ErrInvalid
	}
	if a.Actor.ID == recipient || len(note) < 1 || len(note) > 280 {
		return connection.Request{}, connection.ErrForbidden
	}
	var accessErr error
	ctx, accessErr = s.messageBindAccess(ctx, a)
	if accessErr != nil {
		return connection.Request{}, accessErr
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return connection.Request{}, ea.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	routeFence, e := startMessageRoute(ctx, tx, a.Actor.ID, recipient)
	if e != nil {
		return connection.Request{}, e
	}
	if !messageRequestAllowed(routeFence) {
		return connection.Request{}, connection.ErrNotFound
	}
	if e = s.lockEntityActionWriter(ctx, tx, a); e != nil {
		return connection.Request{}, e
	}
	var sender string
	if e = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR NO KEY UPDATE`, a.Actor.ID).Scan(&sender); e != nil {
		return connection.Request{}, connection.ErrForbidden
	}
	fence, e := s.checkEntityActionWrite(ctx, tx, a, ea.Ref{Type: "person", ID: recipient}, b)
	if e != nil {
		return connection.Request{}, e
	}
	out, e := createFriendRequestWithFenceTx(ctx, tx, a.Actor.ID, recipient, note, routeFence)
	if e != nil {
		return connection.Request{}, e
	}
	fence.createdRequestID = out.ID
	var exact bool
	if e = tx.QueryRow(ctx, `SELECT count(*)=1 AND bool_and(id=$3::uuid AND note=$4 AND scope='friend' AND state='pending') FROM connection_requests WHERE sender_account_id=$1 AND recipient_account_id=$2 AND state='pending'`, a.Actor.ID, recipient, out.ID, note).Scan(&exact); e != nil || !exact {
		return connection.Request{}, ea.ErrChanged
	}
	if e = s.finishEntityActionWrite(ctx, tx, fence); e != nil {
		return connection.Request{}, e
	}
	if e = finishMessageRoute(ctx, tx, routeFence); e != nil {
		return connection.Request{}, e
	}
	return out, tx.Commit(ctx)
}

// Callers may add source/consent checks in the same transaction. No nested
// transaction may separate an invitation's eligibility from its side effects.
func createFriendRequestTx(ctx context.Context, tx pgx.Tx, senderID, recipientID, note string) (connection.Request, error) {
	if senderID == recipientID || len(note) < 1 || len(note) > 280 {
		return connection.Request{}, connection.ErrForbidden
	}
	f, e := startMessageRoute(ctx, tx, senderID, recipientID)
	if e != nil {
		return connection.Request{}, e
	}
	return createFriendRequestWithFenceTx(ctx, tx, senderID, recipientID, note, f)
}
func createFriendRequestWithFenceTx(ctx context.Context, tx pgx.Tx, senderID, recipientID, note string, routeFence messageRouteFence) (connection.Request, error) {
	if senderID == recipientID || len(note) < 1 || len(note) > 280 {
		return connection.Request{}, connection.ErrForbidden
	}
	if !messageRequestAllowed(routeFence) {
		return connection.Request{}, connection.ErrNotFound
	}
	var sender string
	err := tx.QueryRow(ctx, `SELECT id FROM accounts
	        WHERE id=$1 AND account_type='person' AND status='active' FOR NO KEY UPDATE`, senderID).Scan(&sender)
	if errors.Is(err, pgx.ErrNoRows) {
		return connection.Request{}, connection.ErrForbidden
	}
	if err != nil {
		return connection.Request{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE connection_requests SET state='expired', decided_at=now()
        WHERE state='pending' AND expires_at<=now()
          AND (sender_account_id=$1 OR recipient_account_id=$1)`, senderID)
	if err != nil {
		return connection.Request{}, err
	}
	var eligible bool
	err = tx.QueryRow(ctx, `SELECT EXISTS (
        SELECT 1 FROM accounts recipient
        JOIN user_profiles p ON p.account_id=recipient.id AND p.visibility='public'
        WHERE recipient.id=$2 AND recipient.account_type='person' AND recipient.status='active'
          AND NOT EXISTS (SELECT 1 FROM account_blocks b
              WHERE (b.blocker_account_id=$1 AND b.blocked_account_id=$2) OR
                    (b.blocker_account_id=$2 AND b.blocked_account_id=$1))
          AND NOT EXISTS (SELECT 1 FROM person_ties t
              WHERE t.status='active' AND t.person_a_account_id=LEAST($1::uuid,$2::uuid)
                AND t.person_b_account_id=GREATEST($1::uuid,$2::uuid))
    )`, senderID, recipientID).Scan(&eligible)
	if err != nil {
		return connection.Request{}, err
	}
	if !eligible {
		return connection.Request{}, connection.ErrNotFound
	}
	var pending int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM connection_requests
        WHERE sender_account_id=$1 AND state='pending' AND expires_at>now()`, senderID).Scan(&pending); err != nil {
		return connection.Request{}, err
	}
	if pending >= 10 {
		return connection.Request{}, connection.ErrRateLimit
	}
	request := connection.Request{Direction: "outgoing", OtherAccountID: recipientID,
		Note: note, Scope: "friend", State: "pending"}
	err = tx.QueryRow(ctx, `INSERT INTO connection_requests
        (sender_account_id,recipient_account_id,city_id,note,scope,expires_at)
        VALUES($1,$2,NULL,$3,'friend',now()+interval '7 days')
        RETURNING id,expires_at,created_at`, senderID, recipientID, note).
		Scan(&request.ID, &request.ExpiresAt, &request.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return connection.Request{}, connection.ErrConflict
		}
		return connection.Request{}, err
	}
	if err = messageRecordRequest(ctx, tx, &request, routeFence); err != nil {
		return connection.Request{}, err
	}
	if messageOrdinaryDelivery(routeFence) {
		if _, err := routeNativeNotification(ctx, tx, agentnotification.KindConnectionRequest, request.ID, recipientID); err != nil {
			return connection.Request{}, err
		}
	}
	if _, err := auditExec(ctx, tx, `INSERT INTO audit_events
        (actor_account_id,action,resource_type,resource_id,decision,purpose)
        VALUES($1,'request','connection_request',$2,'allowed','human_friend')`, senderID, request.ID); err != nil {
		return connection.Request{}, err
	}
	if err = finishMessageRoute(ctx, tx, routeFence); err != nil {
		return connection.Request{}, err
	}
	return request, nil
}

func (s *Store) ListRequests(ctx context.Context, ownerID string) ([]connection.Request, error) {
	rows, err := s.pool.Query(ctx, `SELECT r.id,
            CASE WHEN r.sender_account_id = $1 THEN 'outgoing' ELSE 'incoming' END,
            CASE WHEN r.sender_account_id = $1 THEN r.recipient_account_id
                 ELSE r.sender_account_id END,
            CASE WHEN other.status = 'active' AND NOT EXISTS (
                SELECT 1 FROM account_blocks b WHERE
                    (b.blocker_account_id = $1 AND b.blocked_account_id = other.id)
                    OR (b.blocker_account_id = other.id AND b.blocked_account_id = $1))
                THEN CASE WHEN birdtie_agent_profile_field_allowed(other.id,$1::uuid,'displayName')
                    THEN COALESCE(NULLIF(p.display_name,''),NULLIF(other.handle,''),'Birdtie 成员')
                    ELSE 'Birdtie 成员' END ELSE '账号暂不可用' END,
            COALESCE(r.city_id,''),
            CASE WHEN other.status = 'active' AND NOT EXISTS (
                SELECT 1 FROM account_blocks b WHERE
                    (b.blocker_account_id = $1 AND b.blocked_account_id = other.id)
                    OR (b.blocker_account_id = other.id AND b.blocked_account_id = $1))
                THEN r.note ELSE '' END,
            r.scope,
            CASE WHEN r.state = 'pending' AND r.expires_at <= now()
                 THEN 'expired' ELSE r.state END,
            COALESCE(cv.id::text, ''), r.expires_at, r.created_at,COALESCE(pb.disposition,''),CASE WHEN pb.disposition='SCREEN' AND r.state='pending' AND r.expires_at>clock_timestamp() THEN 'PENDING_REVIEW' ELSE '' END
        FROM connection_requests r
        JOIN accounts other ON other.id = CASE WHEN r.sender_account_id = $1
            THEN r.recipient_account_id ELSE r.sender_account_id END
        JOIN user_profiles p ON p.account_id = other.id
        LEFT JOIN conversations cv ON cv.request_id = r.id
 LEFT JOIN connection_request_policy_bindings pb ON pb.request_id=r.id
        WHERE r.sender_account_id = $1 OR r.recipient_account_id = $1
        ORDER BY r.created_at DESC, r.id DESC LIMIT 100`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]connection.Request, 0)
	for rows.Next() {
		var item connection.Request
		if err := rows.Scan(&item.ID, &item.Direction, &item.OtherAccountID,
			&item.OtherName, &item.CityID, &item.Note, &item.Scope, &item.State,
			&item.ConversationID, &item.ExpiresAt, &item.CreatedAt, &item.PolicyDisposition, &item.ScreeningStatus); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) DecideRequest(ctx context.Context, actorID, id, action string) (connection.Request, error) {
 tx,err:=s.pool.BeginTx(ctx,pgx.TxOptions{IsoLevel:pgx.ReadCommitted});if err!=nil{return connection.Request{},err};defer tx.Rollback(ctx)
 value,err:=decideRequestInTx(ctx,tx,actorID,id,action,nil);if err!=nil{return connection.Request{},err}
 if err=tx.Commit(ctx);err!=nil{return connection.Request{},err};return value,nil
}

// Single original writer for old clients and keyed causal decisions.
func decideRequestInTx(ctx context.Context,tx pgx.Tx,actorID,id,action string,operation *connectionDecisionOperation)(connection.Request,error){
 var err error
	if action != "accept" && action != "decline" && action != "withdraw" {
		return connection.Request{}, connection.ErrForbidden
	}
	if err = messageAuthenticateCurrent(ctx, tx, actorID); err != nil {
		return connection.Request{}, err
	}
	var routeSender, routePeer string
	err = tx.QueryRow(ctx, `SELECT sender_account_id,recipient_account_id FROM connection_requests WHERE id=$1 AND (sender_account_id=$2 OR recipient_account_id=$2)`, id, actorID).Scan(&routeSender, &routePeer)
	if errors.Is(err, pgx.ErrNoRows) {
		return connection.Request{}, connection.ErrNotFound
	}
	if err != nil {
		return connection.Request{}, err
	}
	// Bind the CURRENT human, while routing remains the original recipient's preference.
	routeCtx := ctx
	if actorID == routePeer {
		routeCtx = context.WithValue(ctx, messageContextKey{}, (*messageCurrent)(nil))
	}
	routeFence, err := startMessageRoute(routeCtx, tx, routeSender, routePeer)
	if err != nil {
		return connection.Request{}, err
	}
	if err = messageLockCurrent(ctx, tx, actorID); err != nil {
		return connection.Request{}, err
	}
	if action == "accept" && routeFence.route == mp.Block {
		return connection.Request{}, connection.ErrForbidden
	}
	if operation != nil {
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(76043,hashtext($1))`, actorID+":"+operation.id); err != nil { return connection.Request{}, connection.ErrOperationUnavailable }
	}
	var senderID, recipientID, cityID, note, scope, state string
	var expiresAt, createdAt time.Time
	err = tx.QueryRow(ctx, `SELECT sender_account_id, recipient_account_id,
        COALESCE(city_id,''), note, scope, state, expires_at, created_at
        FROM connection_requests WHERE id = $1 FOR UPDATE`, id).
		Scan(&senderID, &recipientID, &cityID, &note, &scope, &state, &expiresAt, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return connection.Request{}, connection.ErrNotFound
	}
	if err != nil {
		return connection.Request{}, err
	}
	if (action == "withdraw" && actorID != senderID) ||
		(action != "withdraw" && actorID != recipientID) {
		return connection.Request{}, connection.ErrNotFound
	}
	if operation != nil {
		prior, e := connectionDecisionPrior(ctx,tx,actorID,id,action,operation)
		if e != nil { return connection.Request{},e }
		if prior { if e=finishMessageRouteForActor(ctx,tx,routeFence,actorID);e!=nil{return connection.Request{},e};return connection.Request{},nil }
	}
	var decisionAt time.Time
	if tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&decisionAt) != nil {
		return connection.Request{}, mp.ErrUnavailable
	}
	if state != "pending" || !expiresAt.After(decisionAt) {
		if operation == nil { return connection.Request{}, connection.ErrConflict }
		reason:="EXPIRED";if state!="pending"{reason="ALREADY_DECIDED"}
		if e:=recordConnectionDecision(ctx,tx,actorID,id,action,scope,"NO_EFFECT",reason,decisionAt,operation);e!=nil{return connection.Request{},e}
		if e:=finishMessageRouteForActor(ctx,tx,routeFence,actorID);e!=nil{return connection.Request{},e};return connection.Request{},nil
	}
	if action == "accept" {
		var eligible bool
		err = tx.QueryRow(ctx, `SELECT EXISTS (
            SELECT 1 FROM accounts a JOIN accounts b ON b.id = $2
            WHERE a.id = $1 AND a.status = 'active' AND b.status = 'active'
              AND NOT EXISTS (SELECT 1 FROM account_blocks block
                  WHERE (block.blocker_account_id = $1 AND block.blocked_account_id = $2)
                     OR (block.blocker_account_id = $2 AND block.blocked_account_id = $1))
        )`, senderID, recipientID).Scan(&eligible)
		if err != nil {
			return connection.Request{}, err
		}
		if !eligible {
			return connection.Request{}, connection.ErrForbidden
		}
	}
	state = map[string]string{"accept": "accepted", "decline": "declined", "withdraw": "withdrawn"}[action]
	_, err = tx.Exec(ctx, `UPDATE connection_requests
        SET state = $2, decided_at = now() WHERE id = $1`, id, state)
	if err != nil {
		return connection.Request{}, err
	}
	var conversationID string
	if state == "accepted" {
		if scope == "friend" {
			command, insertErr := tx.Exec(ctx, `INSERT INTO person_ties
                (person_a_account_id,person_b_account_id,request_id)
				VALUES(LEAST($1::uuid,$2::uuid),GREATEST($1::uuid,$2::uuid),$3)
				ON CONFLICT (person_a_account_id,person_b_account_id) DO UPDATE
				SET request_id=EXCLUDED.request_id,status='active',updated_at=now()
				WHERE person_ties.status='removed'`,
				senderID, recipientID, id)
			if insertErr != nil {
				var pgErr *pgconn.PgError
				if errors.As(insertErr, &pgErr) && pgErr.Code == "23505" {
					return connection.Request{}, connection.ErrConflict
				}
				return connection.Request{}, insertErr
			}
			if command.RowsAffected() != 1 {
				return connection.Request{}, connection.ErrConflict
			}
		} else {
			err = tx.QueryRow(ctx, `INSERT INTO conversations
                (request_id, member_a_account_id, member_b_account_id)
                VALUES ($1, $2, $3) RETURNING id`, id, senderID, recipientID).
				Scan(&conversationID)
			if err != nil {
				return connection.Request{}, err
			}
		}
	}
	if state == "accepted" || state == "declined" {
		_, err = routeNativeNotification(ctx, tx, agentnotification.KindConnectionDecision, id, senderID)
		if err != nil {
			return connection.Request{}, err
		}
	}
	_, err = auditExec(ctx, tx, `INSERT INTO audit_events
        (actor_account_id, action, resource_type, resource_id, decision, purpose)
        VALUES ($1, $2, 'connection_request', $3, 'allowed', 'human_contact')`, actorID, state, id)
	if err != nil {
		return connection.Request{}, err
	}
	if operation != nil {
		if err=recordConnectionDecision(ctx,tx,actorID,id,action,scope,"COMMITTED","",decisionAt,operation);err!=nil{return connection.Request{},err}
	}
	if expiresAt.Before(routeFence.until) {
		routeFence.until = expiresAt
	}
	if err = finishMessageRouteForActor(ctx, tx, routeFence, actorID); err != nil {
		return connection.Request{}, err
	}
	return connection.Request{
		ID: id, CityID: cityID, Note: note, Scope: scope, State: state,
		ConversationID: conversationID, ExpiresAt: expiresAt, CreatedAt: createdAt,
	}, nil
}

func (s *Store) ListTies(ctx context.Context, ownerID string) ([]connection.Tie, error) {
	rows, err := s.pool.Query(ctx, `SELECT t.id,other.id,
        CASE WHEN birdtie_agent_profile_field_allowed(other.id,$1::uuid,'displayName')
            THEN COALESCE(NULLIF(p.display_name,''),NULLIF(other.handle,''),'Birdtie 成员')
            ELSE 'Birdtie 成员' END,t.created_at
        FROM person_ties t
        JOIN connection_requests source_request ON source_request.id=t.request_id
            AND source_request.scope='friend' AND source_request.state='accepted'
            AND LEAST(source_request.sender_account_id,source_request.recipient_account_id)=t.person_a_account_id
            AND GREATEST(source_request.sender_account_id,source_request.recipient_account_id)=t.person_b_account_id
        JOIN accounts own ON own.id=$1 AND own.account_type='person' AND own.status='active'
        JOIN accounts other ON other.id=CASE WHEN t.person_a_account_id=$1
            THEN t.person_b_account_id ELSE t.person_a_account_id END
            AND other.account_type='person' AND other.status='active'
        LEFT JOIN user_profiles p ON p.account_id=other.id
        WHERE t.status='active' AND (t.person_a_account_id=$1 OR t.person_b_account_id=$1)
          AND NOT EXISTS (SELECT 1 FROM account_blocks b
              WHERE (b.blocker_account_id=$1 AND b.blocked_account_id=other.id) OR
                    (b.blocker_account_id=other.id AND b.blocked_account_id=$1))
        ORDER BY t.created_at DESC,t.id DESC LIMIT 100`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]connection.Tie, 0)
	for rows.Next() {
		var tie connection.Tie
		if err := rows.Scan(&tie.ID, &tie.OtherAccountID, &tie.OtherName, &tie.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, tie)
	}
	return out, rows.Err()
}

func (s *Store) RemoveTie(ctx context.Context, ownerID, tieID string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id string
	err = tx.QueryRow(ctx, `UPDATE person_ties t SET status='removed',updated_at=now()
		WHERE t.id=$2 AND t.status='active'
		  AND (t.person_a_account_id=$1 OR t.person_b_account_id=$1)
		RETURNING t.id`, ownerID, tieID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return connection.ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err = auditExec(ctx, tx, `INSERT INTO audit_events
		(actor_account_id,action,resource_type,resource_id,decision,purpose)
		VALUES($1,'remove','person_tie',$2,'allowed','human_friend')`, ownerID, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) ListConversations(ctx context.Context, ownerID string) ([]connection.Conversation, error) {
	rows, err := s.pool.Query(ctx, `SELECT cv.id, other.id,
        CASE WHEN birdtie_agent_profile_field_allowed(other.id,$1::uuid,'displayName')
            THEN COALESCE(NULLIF(p.display_name,''),NULLIF(other.handle,''),'Birdtie 成员')
            ELSE 'Birdtie 成员' END, cv.created_at,
		cms.last_read_at,
		(SELECT count(*) FROM conversation_messages m
		 WHERE m.conversation_id=cv.id AND m.sender_account_id<>$1
		   AND m.created_at>cms.baseline_at
		   AND (cms.last_read_message_id IS NULL OR (m.created_at,m.id)>(
		       SELECT cursor.created_at,cursor.id FROM conversation_messages cursor
		       WHERE cursor.id=cms.last_read_message_id)))
        FROM conversations cv
        JOIN connection_requests r ON r.id = cv.request_id AND r.state = 'accepted'
		AND (r.scope='conversation' OR EXISTS(
			SELECT 1 FROM person_ties t WHERE t.status='active'
			AND t.person_a_account_id=LEAST(cv.member_a_account_id,cv.member_b_account_id)
			AND t.person_b_account_id=GREATEST(cv.member_a_account_id,cv.member_b_account_id)))
		JOIN accounts own ON own.id=$1 AND own.status='active'
		JOIN conversation_member_states cms ON cms.conversation_id=cv.id AND cms.member_account_id=$1
        JOIN accounts other ON other.id = CASE WHEN cv.member_a_account_id = $1
            THEN cv.member_b_account_id ELSE cv.member_a_account_id END
            AND other.status = 'active'
        JOIN user_profiles p ON p.account_id = other.id
        WHERE (cv.member_a_account_id = $1 OR cv.member_b_account_id = $1)
          AND NOT EXISTS (SELECT 1 FROM account_blocks b
              WHERE (b.blocker_account_id = $1 AND b.blocked_account_id = other.id)
                 OR (b.blocker_account_id = other.id AND b.blocked_account_id = $1))
        ORDER BY cv.created_at DESC, cv.id DESC LIMIT 100`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]connection.Conversation, 0)
	for rows.Next() {
		var item connection.Conversation
		if err := rows.Scan(&item.ID, &item.OtherAccountID, &item.OtherName,
			&item.CreatedAt, &item.LastReadAt, &item.UnreadCount); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) MarkRead(ctx context.Context, actorID, conversationID, throughMessageID string) (connection.ReadState, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return connection.ReadState{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := conversationMember(ctx, tx, actorID, conversationID); err != nil {
		return connection.ReadState{}, err
	}
	var throughTime time.Time
	err = tx.QueryRow(ctx, `SELECT created_at FROM conversation_messages
		WHERE id=$2 AND conversation_id=$1`, conversationID, throughMessageID).Scan(&throughTime)
	if errors.Is(err, pgx.ErrNoRows) {
		return connection.ReadState{}, connection.ErrNotFound
	}
	if err != nil {
		return connection.ReadState{}, err
	}
	var previousID *string
	var previousTime *time.Time
	var previousReadAt *time.Time
	err = tx.QueryRow(ctx, `SELECT cms.last_read_message_id,cursor.created_at,cms.last_read_at
		FROM conversation_member_states cms
		LEFT JOIN conversation_messages cursor ON cursor.id=cms.last_read_message_id
		WHERE cms.conversation_id=$1 AND cms.member_account_id=$2 FOR UPDATE OF cms`, conversationID, actorID).
		Scan(&previousID, &previousTime, &previousReadAt)
	if err != nil {
		return connection.ReadState{}, err
	}
	state := connection.ReadState{ConversationID: conversationID, LastReadMessageID: throughMessageID}
	if previousID != nil && previousTime != nil &&
		(previousTime.After(throughTime) || (previousTime.Equal(throughTime) && *previousID >= throughMessageID)) {
		state.LastReadMessageID = *previousID
		state.LastReadAt = *previousReadAt
	} else {
		err = tx.QueryRow(ctx, `UPDATE conversation_member_states SET last_read_message_id=$3,last_read_at=now()
			WHERE conversation_id=$1 AND member_account_id=$2 RETURNING last_read_at`,
			conversationID, actorID, throughMessageID).Scan(&state.LastReadAt)
		if err != nil {
			return connection.ReadState{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return connection.ReadState{}, err
	}
	return state, nil
}

func conversationMember(ctx context.Context, tx pgx.Tx, actorID, id string) (string, error) {
	var otherID string
	err := tx.QueryRow(ctx, `SELECT CASE WHEN cv.member_a_account_id = $1
            THEN cv.member_b_account_id ELSE cv.member_a_account_id END
        FROM conversations cv
        JOIN connection_requests r ON r.id = cv.request_id AND r.state = 'accepted'
		AND (r.scope='conversation' OR EXISTS(
			SELECT 1 FROM person_ties t WHERE t.status='active'
			AND t.person_a_account_id=LEAST(cv.member_a_account_id,cv.member_b_account_id)
			AND t.person_b_account_id=GREATEST(cv.member_a_account_id,cv.member_b_account_id)))
        JOIN accounts own ON own.id = $1 AND own.status = 'active'
        JOIN accounts other ON other.id = CASE WHEN cv.member_a_account_id = $1
            THEN cv.member_b_account_id ELSE cv.member_a_account_id END
            AND other.status = 'active'
        WHERE cv.id = $2 AND (cv.member_a_account_id = $1 OR cv.member_b_account_id = $1)
          AND NOT EXISTS (SELECT 1 FROM account_blocks b
              WHERE (b.blocker_account_id = $1 AND b.blocked_account_id = other.id)
                 OR (b.blocker_account_id = other.id AND b.blocked_account_id = $1))
        FOR UPDATE OF cv`, actorID, id).Scan(&otherID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", connection.ErrNotFound
	}
	return otherID, err
}

func (s *Store) ListMessages(ctx context.Context, actorID, id string) ([]connection.Message, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := conversationMember(ctx, tx, actorID, id); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id, conversation_id, sender_account_id,
		speaker_kind, body, created_at, entity_type, entity_id FROM conversation_messages
        WHERE conversation_id = $1
        ORDER BY created_at DESC, id DESC LIMIT 100`, id)
	if err != nil {
		return nil, err
	}
	items := make([]connection.Message, 0)
	type reference struct{ kind, id *string }
	refs := make([]reference, 0)
	for rows.Next() {
		var item connection.Message
		var ref reference
		if err := rows.Scan(&item.ID, &item.ConversationID, &item.SenderID,
			&item.SpeakerKind, &item.Body, &item.CreatedAt, &ref.kind, &ref.id); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, item)
		refs = append(refs, ref)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i, ref := range refs {
		if ref.kind == nil || ref.id == nil {
			continue
		}
		card := connection.EntityCard{Type: *ref.kind}
		title, err := visibleChatEntity(ctx, tx, actorID, *ref.kind, *ref.id)
		if err != nil {
			return nil, err
		}
		if title != "" {
			card.ID, card.Title, card.Available = *ref.id, title, true
		}
		items[i].Entity = &card
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	return items, nil
}

func (s *Store) SendMessage(ctx context.Context, actorID, id, body string) (connection.Message, error) {
	return s.sendMessage(ctx, actorID, id, body, "", "")
}

func (s *Store) SendEntityMessage(ctx context.Context, actorID, id, body, entityType, entityID string) (connection.Message, error) {
	return s.sendMessage(ctx, actorID, id, body, entityType, entityID)
}

func (s *Store) sendMessage(ctx context.Context, actorID, id, body, entityType, entityID string) (connection.Message, error) {
	return s.sendMessageWithOperation(ctx, actorID, id, body, entityType, entityID, "")
}

func (s *Store) sendMessageWithOperation(ctx context.Context, actorID, id, body, entityType, entityID, operation string) (connection.Message, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return connection.Message{}, err
	}
	defer tx.Rollback(ctx)
	routeFence, err := messageConversationFence(ctx, tx, actorID, id)
	if err != nil {
		return connection.Message{}, err
	}
	// Serialize the per-account hourly limit across all conversations.
	var sender string
	if err := tx.QueryRow(ctx, `SELECT id FROM accounts
		WHERE id = $1 AND status = 'active' FOR NO KEY UPDATE`, actorID).Scan(&sender); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return connection.Message{}, connection.ErrNotFound
		}
		return connection.Message{}, err
	}
	otherID, err := conversationMember(ctx, tx, actorID, id)
	if err != nil {
		return connection.Message{}, err
	}
	if operation != "" {
		existing, e := readHumanMessageOperation(ctx, tx, actorID, operation)
		if e == nil {
			if existing.ConversationID != id || existing.Body != body || existing.SenderID != actorID {
				return connection.Message{}, connection.ErrConflict
			}
			if e = finishMessageRoute(ctx, tx, routeFence); e != nil { return connection.Message{}, e }
			if ctx.Err() != nil { return connection.Message{}, ctx.Err() }
			if e = tx.Commit(ctx); e != nil { return connection.Message{}, e }
			return existing, nil
		}
		if !errors.Is(e, pgx.ErrNoRows) { return connection.Message{}, e }
	}
	var entityCard *connection.EntityCard
	if entityType != "" {
		for _, viewer := range []string{actorID, otherID} {
			title, err := visibleChatEntity(ctx, tx, viewer, entityType, entityID)
			if err != nil {
				return connection.Message{}, err
			}
			if title == "" {
				return connection.Message{}, connection.ErrNotFound
			}
			// Each recipient is checked independently; the write response belongs
			// only to its sender and must not reuse the other viewer's label.
			if viewer == actorID {
				entityCard = &connection.EntityCard{Type: entityType, ID: entityID, Title: title, Available: true}
			}
		}
	}
	var recent int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM conversation_messages
        WHERE sender_account_id = $1 AND created_at > now() - interval '1 hour'`, actorID).Scan(&recent)
	if err != nil {
		return connection.Message{}, err
	}
	if recent >= 60 {
		return connection.Message{}, connection.ErrRateLimit
	}
	var item connection.Message
	if operation == "" {
		err = tx.QueryRow(ctx, `INSERT INTO conversation_messages
		(conversation_id, sender_account_id, body, entity_type, entity_id)
		VALUES ($1, $2, $3, NULLIF($4,''), NULLIF($5,'')::uuid)
		RETURNING id, conversation_id, sender_account_id, speaker_kind, body, created_at`,
		id, actorID, body, entityType, entityID).Scan(&item.ID, &item.ConversationID,
		&item.SenderID, &item.SpeakerKind, &item.Body, &item.CreatedAt)
	} else {
		err = tx.QueryRow(ctx, `INSERT INTO conversation_messages(conversation_id,sender_account_id,body,client_operation_id)
		VALUES($1,$2,$3,$4) RETURNING id,conversation_id,sender_account_id,speaker_kind,body,created_at`,
		id,actorID,body,operation).Scan(&item.ID,&item.ConversationID,&item.SenderID,&item.SpeakerKind,&item.Body,&item.CreatedAt)
	}
	if err != nil {
		return connection.Message{}, err
	}
	_, err = routeNativeNotification(ctx, tx, agentnotification.KindDirectMessage, item.ID, otherID)
	if err != nil {
		return connection.Message{}, err
	}
	if err = finishMessageRoute(ctx, tx, routeFence); err != nil {
		return connection.Message{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return connection.Message{}, err
	}
	if ctx.Err() != nil { return connection.Message{}, ctx.Err() }
	item.Entity = entityCard
	return item, nil
}

// Only published objects visible to this participant may resolve into a card.
// The same check runs on send for both peers and again on every read.
func chatEntityQuery(kind string) string {
	var query string
	switch kind {
	case "activity":
		query = `SELECT a.title FROM activities a JOIN cities c ON c.id=a.city_id
		    WHERE a.id=$1 AND a.publication_status='published' AND c.publication_status='published'
		    AND a.cancelled_at IS NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
            AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp())
		    AND birdtie_activity_visible_to(a.id,$2::uuid)
		    AND NOT EXISTS (SELECT 1 FROM account_blocks b WHERE
		      (b.blocker_account_id=$2 AND b.blocked_account_id=a.host_account_id) OR
		      (b.blocked_account_id=$2 AND b.blocker_account_id=a.host_account_id))`
	case "place":
		query = `SELECT p.name AS title FROM places p JOIN cities c ON c.id=p.city_id
		    WHERE p.id=$1 AND p.publication_status='published' AND c.publication_status='published'
		    AND (p.expires_at IS NULL OR p.expires_at>clock_timestamp())
            AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp())`
	case "person":
		query = `SELECT CASE WHEN birdtie_agent_profile_field_allowed(a.id,$2::uuid,'displayName')
                THEN COALESCE(NULLIF(p.display_name,''),NULLIF(a.handle,''),'Birdtie 成员')
                ELSE 'Birdtie 成员' END AS title FROM user_profiles p JOIN accounts a ON a.id=p.account_id
		    WHERE p.account_id=$1 AND p.visibility='public' AND a.status='active' AND a.account_type='person'
            AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE (b.blocker_account_id=$2 AND b.blocked_account_id=a.id)
              OR (b.blocker_account_id=a.id AND b.blocked_account_id=$2))`
	case "community":
		query = `SELECT co.name AS title FROM communities co JOIN accounts creator ON creator.id=co.owner_account_id
            WHERE co.id=$1 AND co.publication_status='published' AND co.visibility='public' AND co.lifecycle_status='active'
            AND creator.status='active' AND creator.account_type='person'
            AND (co.expires_at IS NULL OR co.expires_at>clock_timestamp())
            AND (co.city_id IS NULL OR EXISTS(SELECT 1 FROM cities c WHERE c.id=co.city_id AND c.publication_status='published'
              AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp())))
            AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE (b.blocker_account_id=$2 AND b.blocked_account_id=creator.id)
              OR (b.blocker_account_id=creator.id AND b.blocked_account_id=$2))`
	case "organization":
		query = `SELECT o.name AS title FROM organizations o JOIN accounts a ON a.id=o.account_id
		    WHERE o.id=$1 AND o.visibility='public' AND o.status='active'
		    AND o.verification_status='verified' AND a.status='active' AND a.account_type='organization'
            AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE (b.blocker_account_id=$2 AND b.blocked_account_id=a.id)
              OR (b.blocker_account_id=a.id AND b.blocked_account_id=$2))`
	case "business":
		query = `SELECT b.name AS title FROM businesses b JOIN accounts a ON a.id=b.account_id
		    WHERE b.id=$1 AND b.status='active' AND b.claim_status='verified' AND a.status='active' AND a.account_type='business'
            AND NOT EXISTS(SELECT 1 FROM account_blocks block WHERE (block.blocker_account_id=$2 AND block.blocked_account_id=a.id)
              OR (block.blocker_account_id=a.id AND block.blocked_account_id=$2))`
	case "moment":
		query = `SELECT m.title FROM moments m JOIN accounts a ON a.id=m.author_account_id
            JOIN places p ON p.id=m.place_id AND p.city_id=m.city_id JOIN cities c ON c.id=p.city_id
		    WHERE m.id=$1 AND m.status='published' AND m.visibility='public' AND a.status='active' AND a.account_type='person'
            AND m.location_precision='place' AND m.author_confirmed_at IS NOT NULL AND m.published_at IS NOT NULL
            AND m.author_confirmed_at<=m.published_at AND m.published_at<=clock_timestamp()
            AND p.publication_status='published' AND c.publication_status='published'
            AND (p.expires_at IS NULL OR p.expires_at>clock_timestamp()) AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp())
            AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE (b.blocker_account_id=$2 AND b.blocked_account_id=a.id)
              OR (b.blocker_account_id=a.id AND b.blocked_account_id=$2))`
	default:
		return ""
	}
	return query
}

func visibleChatEntity(ctx context.Context, tx pgx.Tx, viewerID, kind, entityID string) (string, error) {
	query := chatEntityQuery(kind)
	if query == "" {
		return "", connection.ErrNotFound
	}
	var title string
	args := []any{entityID}
	if kind != "place" {
		args = append(args, viewerID)
	}
	err := tx.QueryRow(ctx, query, args...).Scan(&title)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return title, err
}
