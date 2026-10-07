package postgres

import (
	"context"
	"errors"
	mp "github.com/birdtie/birdtie/apps/api/internal/agentmessagepolicy"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"time"
)

type messageContextKey struct{}
type messageCurrent struct {
	access  ea.Access
	version string
	dev     bool
}

func messageWithCurrent(ctx context.Context, a identity.Actor, digest [32]byte, v string) context.Context {
	return context.WithValue(ctx, messageContextKey{}, &messageCurrent{access: ea.Access{Actor: a, SessionDigest: digest}, version: v})
}
func messageCurrentContext(ctx context.Context) *messageCurrent {
	a, _ := ctx.Value(messageContextKey{}).(*messageCurrent)
	return a
}
func (s *Store) messageCurrentAccess(ctx context.Context, a ea.Access, v string) (context.Context, error) {
	if !a.Valid() || a.Actor.AccountType != "person" {
		return ctx, identity.ErrUnauthorized
	}
	if v != "" && !mp.ValidSourceVersion(v) {
		return ctx, mp.ErrInvalid
	}
	if current := messageCurrentContext(ctx); current != nil && (current.access.Actor.ID != a.Actor.ID || current.access.Actor.AccountType != a.Actor.AccountType || current.access.SessionDigest != a.SessionDigest) {
		return ctx, identity.ErrUnauthorized
	}
	c := messageWithCurrent(ctx, a.Actor, a.SessionDigest, v)
	messageCurrentContext(c).dev = s.devPhoneEnabled
	return c, nil
}
func (s *Store) messageBindAccess(ctx context.Context, a ea.Access) (context.Context, error) {
	v := ""
	if current := messageCurrentContext(ctx); current != nil {
		v = current.version
	}
	return s.messageCurrentAccess(ctx, a, v)
}

var _ connection.CurrentStore = (*Store)(nil)

func (s *Store) CreateRequestCurrent(ctx context.Context, a ea.Access, peer, city, note, v string, b *ea.BoundCondition) (connection.Request, error) {
	ctx, e := s.messageCurrentAccess(ctx, a, v)
	if e != nil {
		return connection.Request{}, e
	}
	if b != nil {
		return s.CreateRequestBound(ctx, a, peer, city, note, *b)
	}
	return s.CreateRequest(ctx, a.Actor.ID, peer, city, note)
}
func (s *Store) CreateFriendRequestCurrent(ctx context.Context, a ea.Access, peer, note, v string, b *ea.BoundCondition) (connection.Request, error) {
	ctx, e := s.messageCurrentAccess(ctx, a, v)
	if e != nil {
		return connection.Request{}, e
	}
	if b != nil {
		return s.CreateFriendRequestBound(ctx, a, peer, note, *b)
	}
	return s.CreateFriendRequest(ctx, a.Actor.ID, peer, note)
}
func (s *Store) DecideRequestCurrent(ctx context.Context, a ea.Access, id, action string) (connection.Request, error) {
	ctx, e := s.messageCurrentAccess(ctx, a, "")
	if e != nil {
		return connection.Request{}, e
	}
	return s.DecideRequest(ctx, a.Actor.ID, id, action)
}
func (s *Store) StartFriendConversationCurrent(ctx context.Context, a ea.Access, id string, b *ea.BoundCondition) (connection.Conversation, error) {
	ctx, e := s.messageCurrentAccess(ctx, a, "")
	if e != nil {
		return connection.Conversation{}, e
	}
	if b != nil {
		return s.StartFriendConversationBound(ctx, a, id, *b)
	}
	return s.StartFriendConversation(ctx, a.Actor.ID, id)
}
func (s *Store) SendMessageCurrent(ctx context.Context, a ea.Access, id, body, kind, entity string) (connection.Message, error) {
	ctx, e := s.messageCurrentAccess(ctx, a, "")
	if e != nil {
		return connection.Message{}, e
	}
	return s.sendMessage(ctx, a.Actor.ID, id, body, kind, entity)
}

func messageAuthenticateCurrent(ctx context.Context, tx pgx.Tx, actor string) error {
	a := messageCurrentContext(ctx)
	if a == nil {
		return nil
	}
	if a.access.Actor.ID != actor || a.access.Actor.AccountType != "person" || !a.access.Valid() {
		return identity.ErrUnauthorized
	}
	return checkHumanMomentSession(ctx, tx, a.access.SessionDigest, actor, a.dev)
}
func messageLockCurrent(ctx context.Context, tx pgx.Tx, actor string) error {
	a := messageCurrentContext(ctx)
	if a == nil {
		return nil
	}
	if e := messageAuthenticateCurrent(ctx, tx, actor); e != nil {
		return e
	}
	var id string
	e := tx.QueryRow(ctx, `SELECT id FROM sessions WHERE token_sha256=$1 AND account_id=$2 AND revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_expires_at>clock_timestamp() AND ($3::boolean OR authentication_method<>'dev_phone') FOR SHARE`, a.access.SessionDigest[:], actor, a.dev).Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		return identity.ErrUnauthorized
	}
	if e != nil || ctx.Err() != nil {
		return mp.ErrUnavailable
	}
	return nil
}
func messageRecordRequest(ctx context.Context, tx pgx.Tx, r *connection.Request, f messageRouteFence) error {
	if !f.configured {
		return nil
	}
	if f.route != mp.Request && f.route != mp.Screen {
		return connection.ErrNotFound
	}
	_, e := tx.Exec(ctx, `INSERT INTO connection_request_policy_bindings(request_id,recipient_id,disposition,policy_version,observed_at) VALUES($1,$2,$3,$4,clock_timestamp())`, r.ID, f.peer, f.route, f.version)
	if e != nil {
		return mp.ErrUnavailable
	}
	r.PolicyDisposition = string(f.route)
	if f.route == mp.Screen {
		r.ScreeningStatus = "PENDING_REVIEW"
	}
	return nil
}
func messageRequestAllowed(f messageRouteFence) bool {
	return f.route == mp.Request || f.route == mp.Screen
}
func messageOrdinaryDelivery(f messageRouteFence) bool { return f.route == mp.Request }

// Specific accepted legacy Conversation consent remains its original ACL.
// It is not converted into a person-wide ALLOW or an Agent permission.
func messageConversationFence(ctx context.Context, tx pgx.Tx, actor, id string) (messageRouteFence, error) {
	if e := messageAuthenticateCurrent(ctx, tx, actor); e != nil {
		return messageRouteFence{}, e
	}
	var peer string
	var scoped bool
	e := tx.QueryRow(ctx, `SELECT CASE WHEN cv.member_a_account_id=$1 THEN cv.member_b_account_id ELSE cv.member_a_account_id END,r.scope='conversation' FROM conversations cv JOIN connection_requests r ON r.id=cv.request_id AND r.state='accepted' AND LEAST(r.sender_account_id,r.recipient_account_id)=LEAST(cv.member_a_account_id,cv.member_b_account_id) AND GREATEST(r.sender_account_id,r.recipient_account_id)=GREATEST(cv.member_a_account_id,cv.member_b_account_id) WHERE cv.id=$2 AND (cv.member_a_account_id=$1 OR cv.member_b_account_id=$1)`, actor, id).Scan(&peer, &scoped)
	if errors.Is(e, pgx.ErrNoRows) {
		return messageRouteFence{}, connection.ErrNotFound
	}
	if e != nil {
		return messageRouteFence{}, mp.ErrUnavailable
	}
	f, e := startMessageRoute(ctx, tx, actor, peer)
	if e != nil {
		return f, e
	}
	if f.blocked || (!scoped && f.route != mp.Allow) {
		return f, connection.ErrNotFound
	}
	if scoped {
		f.until = f.checkedAt.Add(30 * time.Second)
	} // Inbox preference expiry does not revoke existing scoped consent.
	return f, nil
}
