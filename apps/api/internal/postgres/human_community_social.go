package postgres

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/community"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

var _ community.HumanSocialStore = (*Store)(nil)
var communityHumanUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

const communityApprovalTTL = 30 * time.Second

type communityHumanAccess struct {
	tx                           pgx.Tx
	actor, session, accountEpoch string
	digest                       [32]byte
}

// One current READ COMMITTED transaction owns both authorization and domain
// changes. The legacy internal actor-ID methods are never an HTTP fallback.
func (s *Store) beginHumanCommunity(ctx context.Context, digest [32]byte, actor identity.Actor) (communityHumanAccess, error) {
	if ctx == nil || s == nil || s.pool == nil {
		return communityHumanAccess{}, community.ErrUnavailable
	}
	if ctx.Err() != nil {
		return communityHumanAccess{}, community.ErrUnavailable
	}
	if actor.AccountType != "person" || !communityHumanUUID.MatchString(actor.ID) || digest == ([32]byte{}) {
		return communityHumanAccess{}, identity.ErrUnauthorized
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return communityHumanAccess{}, community.ErrUnavailable
	}
	fail := func(e error) (communityHumanAccess, error) {
		tx.Rollback(context.Background())
		return communityHumanAccess{}, e
	}
	if _, err = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); err != nil {
		return fail(community.ErrUnavailable)
	}
	a := communityHumanAccess{tx: tx, actor: actor.ID, digest: digest}
	err = tx.QueryRow(ctx, `SELECT xmin::text FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR SHARE`, actor.ID).Scan(&a.accountEpoch)
	if errors.Is(err, pgx.ErrNoRows) {
		return fail(identity.ErrUnauthorized)
	}
	if err != nil {
		return fail(community.ErrUnavailable)
	}
	err = tx.QueryRow(ctx, `SELECT id FROM sessions WHERE token_sha256=$1 AND account_id=$2 AND revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_expires_at>clock_timestamp() AND ($3::boolean OR authentication_method<>'dev_phone') FOR SHARE`, digest[:], actor.ID, s.devPhoneEnabled).Scan(&a.session)
	if errors.Is(err, pgx.ErrNoRows) {
		return fail(identity.ErrUnauthorized)
	}
	if err != nil {
		return fail(community.ErrUnavailable)
	}
	return a, nil
}
func (s *Store) finishHumanCommunity(ctx context.Context, a communityHumanAccess, id string, fences ...*entityActionWriteFence) error {
	var live bool
	err := a.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sessions ss JOIN accounts aa ON aa.id=ss.account_id WHERE ss.id=$1 AND ss.token_sha256=$2 AND aa.id=$3 AND aa.account_type='person' AND aa.status='active' AND aa.xmin::text=$4 AND ss.revoked_at IS NULL AND ss.expires_at>clock_timestamp() AND ss.idle_expires_at>clock_timestamp() AND ($5::boolean OR ss.authentication_method<>'dev_phone'))`, a.session, a.digest[:], a.actor, a.accountEpoch, s.devPhoneEnabled).Scan(&live)
	if err != nil || ctx.Err() != nil {
		return community.ErrUnavailable
	}
	if !live {
		return identity.ErrUnauthorized
	}
	if id != "" {
		err = a.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM communities WHERE id=$1 AND (expires_at IS NULL OR expires_at>clock_timestamp()))`, id).Scan(&live)
		if err != nil {
			return community.ErrUnavailable
		}
		if !live {
			return community.ErrNotFound
		}
	}
	if ctx.Err() != nil {
		return community.ErrUnavailable
	}
	if len(fences) == 1 && fences[0] != nil {
		if e := s.finishEntityActionWrite(ctx, a.tx, *fences[0]); e != nil {
			return e
		}
	}
	if err = a.tx.Commit(ctx); err != nil {
		return community.ErrUnavailable
	}
	return nil
}
func communityHumanError(e error) error {
	if e == nil {
		return nil
	}
	for _, known := range []error{identity.ErrUnauthorized, community.ErrNotFound, community.ErrForbidden, community.ErrConflict, community.ErrValidation, community.ErrApprovalRequired, community.ErrApprovalStale} {
		if errors.Is(e, known) {
			return known
		}
	}
	return community.ErrUnavailable
}
func (s *Store) ReadHumanCommunity(ctx context.Context, digest [32]byte, actor identity.Actor, in community.HumanRead) (community.HumanResult, error) {
	var out community.HumanResult
	if in.Kind != "list" && in.Kind != "detail" && in.Kind != "members" {
		return out, community.ErrValidation
	}
	if len(in.CityID) > 80 || (in.Kind != "list" && !communityHumanUUID.MatchString(in.CommunityID)) {
		return out, community.ErrValidation
	}
	a, e := s.beginHumanCommunity(ctx, digest, actor)
	if e != nil {
		return out, e
	}
	defer a.tx.Rollback(context.Background())
	if in.Kind != "list" {
		var id string
		e = a.tx.QueryRow(ctx, `SELECT id FROM communities WHERE id=$1 AND lifecycle_status='active' AND publication_status='published' AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE`, in.CommunityID).Scan(&id)
		if errors.Is(e, pgx.ErrNoRows) {
			return out, community.ErrNotFound
		}
		if e != nil {
			return out, community.ErrUnavailable
		}
		// The active viewer's role cannot be removed while private details/member
		// names are read. Display-name visibility still uses the existing resolver.
		rows, err := a.tx.Query(ctx, `SELECT id FROM community_memberships WHERE community_id=$1 AND user_account_id=$2 FOR SHARE`, id, a.actor)
		if err != nil {
			return out, community.ErrUnavailable
		}
		for rows.Next() {
			var v string
			if err = rows.Scan(&v); err != nil {
				rows.Close()
				return out, community.ErrUnavailable
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, community.ErrUnavailable
		}
	}
	switch in.Kind {
	case "list":
		out.Communities, e = listSocialCommunitiesInTx(ctx, a.tx, a.actor, in.CityID, in.Mine)
	case "detail":
		var r community.SocialRecord
		r, e = getSocialCommunityInTx(ctx, a.tx, a.actor, in.CommunityID)
		if e == nil {
			out.Community = &r
		}
	case "members":
		out.Members, e = listSocialMembersInTx(ctx, a.tx, a.actor, in.CommunityID, in.Requests, true)
	}
	if e != nil {
		return community.HumanResult{}, communityHumanError(e)
	}
	if e = s.finishHumanCommunity(ctx, a, in.CommunityID); e != nil {
		return community.HumanResult{}, e
	}
	return out, nil
}
func validateHumanCommunityCommand(in community.HumanCommand) error {
	switch in.Operation {
	case "create", "update", "archive", "join", "leave", "approve", "reject", "invite", "role", "remove", "transfer":
	default:
		return community.ErrValidation
	}
	if in.Operation != "create" && !communityHumanUUID.MatchString(in.CommunityID) {
		return community.ErrValidation
	}
	if in.Preview && !community.RequiresConfirmation(in.Operation) {
		return community.ErrValidation
	}
	if len(in.Snapshot) > 200 {
		return community.ErrApprovalStale
	}
	switch in.Operation {
	case "approve", "reject", "invite", "role", "remove", "transfer":
		if !communityHumanUUID.MatchString(in.TargetID) {
			return community.ErrValidation
		}
	}
	if in.Operation == "role" && in.Role != "admin" && in.Role != "member" {
		return community.ErrValidation
	}
	if in.Operation == "create" || in.Operation == "update" {
		p := in.Input
		if !utf8.ValidString(p.Name) || !utf8.ValidString(p.Summary) || strings.TrimSpace(p.Name) != p.Name || len([]rune(p.Name)) < 2 || len([]rune(p.Name)) > 160 || len([]rune(p.Summary)) > 3000 || len(p.CityID) > 80 {
			return community.ErrValidation
		}
		if p.Visibility != "public" && p.Visibility != "private" && p.Visibility != "hidden" {
			return community.ErrValidation
		}
		if p.JoinPolicy != "open" && p.JoinPolicy != "request" && p.JoinPolicy != "invite_only" {
			return community.ErrValidation
		}
		if p.AvatarURL != "" {
			u, e := url.Parse(p.AvatarURL)
			if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || len(p.AvatarURL) > 1000 {
				return community.ErrValidation
			}
		}
	}
	return nil
}

// All snapshot material is server-read native metadata. No private text,
// session digest, xmin or membership JSON is returned in a confirmation token.
func communityApprovalMaterial(ctx context.Context, a communityHumanAccess, in community.HumanCommand, lockedTarget string) (string, error) {
	var group, own, targetAccount, targetMember string
	e := a.tx.QueryRow(ctx, `SELECT jsonb_build_array(to_jsonb(c),c.xmin::text)::text FROM communities c WHERE id=$1`, in.CommunityID).Scan(&group)
	if e != nil {
		return "", e
	}
	e = a.tx.QueryRow(ctx, `SELECT jsonb_build_array(to_jsonb(m),m.xmin::text)::text FROM community_memberships m WHERE community_id=$1 AND user_account_id=$2 FOR SHARE`, in.CommunityID, a.actor).Scan(&own)
	if e != nil {
		return "", community.ErrForbidden
	}
	role, status, e := socialRole(ctx, a.tx, a.actor, in.CommunityID)
	if e != nil {
		return "", e
	}
	if status != "active" || (role != "owner" && role != "admin") {
		return "", community.ErrForbidden
	}
	if (in.Operation == "archive" || in.Operation == "role" || in.Operation == "transfer") && role != "owner" {
		return "", community.ErrForbidden
	}
	var targetRole, targetStatus string
	if in.TargetID != "" {
		var targetUser string
		if in.Operation == "invite" || in.Operation == "transfer" {
			targetUser = in.TargetID
		} else {
			e = a.tx.QueryRow(ctx, `SELECT user_account_id FROM community_memberships WHERE id=$1 AND community_id=$2 FOR SHARE`, in.TargetID, in.CommunityID).Scan(&targetUser)
			if errors.Is(e, pgx.ErrNoRows) {
				return "", community.ErrNotFound
			}
			if e != nil {
				return "", e
			}
		}
		if targetUser != lockedTarget {
			return "", community.ErrApprovalStale
		}
		// Target identity was share-locked before Community. Recheck exact binding
		// here; IDs alone are not permissions or invitations.
		e = a.tx.QueryRow(ctx, `SELECT jsonb_build_array(id,status,account_type,xmin::text)::text FROM accounts WHERE id=$1 AND account_type='person' AND status='active'`, targetUser).Scan(&targetAccount)
		if errors.Is(e, pgx.ErrNoRows) {
			return "", community.ErrNotFound
		}
		if e != nil {
			return "", e
		}
		e = a.tx.QueryRow(ctx, `SELECT jsonb_build_array(to_jsonb(m),m.xmin::text)::text,role,status FROM community_memberships m WHERE community_id=$1 AND user_account_id=$2 FOR SHARE`, in.CommunityID, targetUser).Scan(&targetMember, &targetRole, &targetStatus)
		if errors.Is(e, pgx.ErrNoRows) && in.Operation == "invite" {
			e = nil
		}
		if errors.Is(e, pgx.ErrNoRows) {
			return "", community.ErrNotFound
		}
		if e != nil {
			return "", e
		}
		switch in.Operation {
		case "invite":
			if targetStatus == "active" || targetStatus == "pending" || targetStatus == "invited" {
				return "", community.ErrConflict
			}
		case "approve", "reject":
			if targetStatus != "pending" {
				return "", community.ErrConflict
			}
		case "role":
			if targetStatus != "active" || (targetRole != "admin" && targetRole != "member") {
				return "", community.ErrConflict
			}
		case "remove":
			if targetRole == "owner" || (role == "admin" && targetRole != "member") {
				return "", community.ErrForbidden
			}
			if targetStatus != "active" && targetStatus != "pending" && targetStatus != "invited" {
				return "", community.ErrConflict
			}
		case "transfer":
			if targetUser == a.actor || targetStatus != "active" {
				return "", community.ErrConflict
			}
		}
	}
	raw, e := json.Marshal([]any{"COMMUNITY_HUMAN_CURRENT_V1", a.actor, a.accountEpoch, a.session, in.Operation, in.CommunityID, in.TargetID, in.Role, in.Input, group, own, targetAccount, targetMember})
	return string(raw), e
}
func communityApprovalToken(digest [32]byte, material string, issued, expiry int64) string {
	h := hmac.New(sha256.New, digest[:])
	fmt.Fprintf(h, "%d:%d:%s", issued, expiry, material)
	return fmt.Sprintf("cm1.%d.%d.%s", issued, expiry, hex.EncodeToString(h.Sum(nil)))
}
func (s *Store) MutateHumanCommunity(ctx context.Context, digest [32]byte, actor identity.Actor, in community.HumanCommand) (community.HumanResult, error) {
	var out community.HumanResult
	if e := validateHumanCommunityCommand(in); e != nil {
		return out, e
	}
	a, e := s.beginHumanCommunity(ctx, digest, actor)
	if e != nil {
		return out, e
	}
	defer a.tx.Rollback(context.Background())
	lockedTarget := ""
	approvalExpiry := int64(0)
	if in.TargetID != "" {
		target := in.TargetID
		if in.Operation != "invite" && in.Operation != "transfer" {
			e = a.tx.QueryRow(ctx, `SELECT user_account_id FROM community_memberships WHERE id=$1 AND community_id=$2`, in.TargetID, in.CommunityID).Scan(&target)
			if errors.Is(e, pgx.ErrNoRows) {
				return out, community.ErrNotFound
			}
			if e != nil {
				return out, community.ErrUnavailable
			}
		}
		var locked string
		e = a.tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR SHARE`, target).Scan(&locked)
		if errors.Is(e, pgx.ErrNoRows) {
			return out, community.ErrNotFound
		}
		if e != nil {
			return out, community.ErrUnavailable
		}
		lockedTarget = locked
	}
	if in.Operation != "create" {
		if e = socialLockCommunity(ctx, a.tx, in.CommunityID); e != nil {
			return out, communityHumanError(e)
		}
	}
	var actionFence *entityActionWriteFence
	var previousMemberID string
	if in.ActionCondition != nil {
		if (in.Operation != "join" && in.Operation != "leave") || in.ActionCondition.Kind != ea.Join ||
			(in.Operation == "join" && (in.ActionCondition.Operation != "JOIN" && in.ActionCondition.Operation != "REQUEST_JOIN" && in.ActionCondition.Operation != "ACCEPT_INVITATION")) ||
			(in.Operation == "leave" && (in.ActionCondition.Operation != "LEAVE" && in.ActionCondition.Operation != "DECLINE_INVITATION")) {
			return out, ea.ErrInvalid
		}
		access := ea.Access{Actor: actor, SessionDigest: digest}
		if e = s.lockEntityActionWriter(ctx, a.tx, access); e != nil {
			return out, e
		}
		e = a.tx.QueryRow(ctx, `SELECT id FROM community_memberships WHERE community_id=$1 AND user_account_id=$2 FOR UPDATE`, in.CommunityID, a.actor).Scan(&previousMemberID)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return out, community.ErrUnavailable
		}
		fence, err := s.checkEntityActionWrite(ctx, a.tx, access, ea.Ref{Type: "community", ID: in.CommunityID}, *in.ActionCondition)
		if err != nil {
			return out, err
		}
		actionFence = &fence
	}
	if community.RequiresConfirmation(in.Operation) {
		material, err := communityApprovalMaterial(ctx, a, in, lockedTarget)
		if err != nil {
			return out, communityHumanError(err)
		}
		var now time.Time
		if e = a.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
			return out, community.ErrUnavailable
		}
		if in.Preview {
			expiry := now.Add(communityApprovalTTL)
			out.Approval = &community.HumanApproval{Snapshot: communityApprovalToken(digest, material, now.UnixNano(), expiry.UnixNano()), ActorID: a.actor, CommunityID: in.CommunityID, TargetID: in.TargetID, Operation: in.Operation, Role: in.Role, ExpiresAt: expiry}
			if e = s.finishHumanCommunity(ctx, a, in.CommunityID); e != nil {
				return community.HumanResult{}, e
			}
			return out, nil
		}
		if in.Snapshot == "" {
			return out, community.ErrApprovalRequired
		}
		parts := strings.Split(in.Snapshot, ".")
		if len(parts) != 4 || parts[0] != "cm1" {
			return out, community.ErrApprovalStale
		}
		issued, e1 := strconv.ParseInt(parts[1], 10, 64)
		expiry, e2 := strconv.ParseInt(parts[2], 10, 64)
		if e1 != nil || e2 != nil || issued <= 0 || expiry <= issued || expiry-issued != int64(communityApprovalTTL) || issued > now.UnixNano() || expiry <= now.UnixNano() || !hmac.Equal([]byte(in.Snapshot), []byte(communityApprovalToken(digest, material, issued, expiry))) {
			return out, community.ErrApprovalStale
		}
		approvalExpiry = expiry
	}
	var c community.SocialRecord
	var m community.Membership
	switch in.Operation {
	case "create":
		c, e = createSocialCommunityInTx(ctx, a.tx, a.actor, in.Input)
		if e == nil {
			out.Community = &c
		}
	case "update":
		c, e = updateSocialCommunityInTx(ctx, a.tx, a.actor, in.CommunityID, in.Input)
		if e == nil {
			out.Community = &c
		}
	case "archive":
		e = archiveSocialCommunityInTx(ctx, a.tx, a.actor, in.CommunityID)
	case "join":
		m, e = joinSocialCommunityInTx(ctx, a.tx, a.actor, in.CommunityID)
		if e == nil {
			out.Member = &m
		}
	case "leave":
		e = leaveSocialCommunityInTx(ctx, a.tx, a.actor, in.CommunityID)
	case "approve", "reject":
		m, e = decideSocialRequestInTx(ctx, a.tx, a.actor, in.CommunityID, in.TargetID, in.Operation == "approve")
		if e == nil {
			out.Member = &m
		}
	case "invite":
		m, e = inviteSocialMemberInTx(ctx, a.tx, a.actor, in.CommunityID, in.TargetID)
		if e == nil {
			out.Member = &m
		}
	case "role":
		m, e = changeSocialMemberRoleInTx(ctx, a.tx, a.actor, in.CommunityID, in.TargetID, in.Role)
		if e == nil {
			out.Member = &m
		}
	case "remove":
		e = removeSocialMemberInTx(ctx, a.tx, a.actor, in.CommunityID, in.TargetID)
	case "transfer":
		e = transferSocialOwnerInTx(ctx, a.tx, a.actor, in.CommunityID, in.TargetID)
	}
	if e != nil {
		return community.HumanResult{}, communityHumanError(e)
	}
	if actionFence != nil {
		want := "left"
		memberID := previousMemberID
		if in.Operation == "join" {
			if out.Member == nil {
				return community.HumanResult{}, ea.ErrChanged
			}
			want = "active"
			if in.ActionCondition.Operation == "REQUEST_JOIN" {
				want = "pending"
			}
			memberID = out.Member.ID
			if previousMemberID != "" && memberID != previousMemberID {
				return community.HumanResult{}, ea.ErrChanged
			}
		}
		var exact bool
		if e = a.tx.QueryRow(ctx, `SELECT count(*)=1 AND bool_and(id=$3::uuid AND status=$4) FROM community_memberships WHERE community_id=$1 AND user_account_id=$2`, in.CommunityID, a.actor, memberID, want).Scan(&exact); e != nil || !exact {
			return community.HumanResult{}, ea.ErrChanged
		}
	}
	if approvalExpiry != 0 {
		var now time.Time
		if e = a.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
			return community.HumanResult{}, community.ErrUnavailable
		}
		if now.UnixNano() >= approvalExpiry {
			return community.HumanResult{}, community.ErrApprovalStale
		}
	}
	if e = s.finishHumanCommunity(ctx, a, in.CommunityID, actionFence); e != nil {
		return community.HumanResult{}, e
	}
	return out, nil
}
