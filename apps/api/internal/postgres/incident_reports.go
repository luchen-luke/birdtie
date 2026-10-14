package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activitychat"
	"github.com/birdtie/birdtie/apps/api/internal/communitychat"
	"github.com/birdtie/birdtie/apps/api/internal/safety"
	"github.com/jackc/pgx/v5"
)

func scanReport(row scanner) (safety.Report, error) {
	var item safety.Report
	err := row.Scan(&item.ID, &item.TargetType, &item.TargetID, &item.Reason,
		&item.Details, &item.Status, &item.CreatedAt)
	return item, err
}

const reportColumns = `id,target_type,target_id,reason,details,status,created_at`

func (s *Store) CreateReport(ctx context.Context, reporterID string, input safety.ReportInput) (safety.Report, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return safety.Report{}, e
	}
	defer tx.Rollback(context.Background())
	out, e := reportEffectTx(ctx, tx, reporterID, input)
	if e != nil {
		return out, e
	}
	if e = tx.Commit(ctx); e != nil {
		return out, e
	}
	return out, nil
}
func reportEffectTx(ctx context.Context, tx pgx.Tx, reporterID string, input safety.ReportInput) (safety.Report, error) {
	var locked string
	if err := tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR UPDATE`, reporterID).Scan(&locked); err != nil {
		return safety.Report{}, err
	}
	var recent int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM incident_reports
		WHERE reporter_account_id=$1 AND created_at>now()-interval '24 hours'`, reporterID).Scan(&recent); err != nil {
		return safety.Report{}, err
	}
	if recent >= 5 {
		return safety.Report{}, safety.ErrRateLimited
	}
	var targetID any
	communityMessageAllowed := false
	if input.TargetType == "community_message" {
		var communityID string
		err := tx.QueryRow(ctx, `SELECT c.community_id FROM community_conversation_messages m
			JOIN community_conversations c ON c.id=m.conversation_id
			JOIN community_conversation_members member ON member.conversation_id=c.id
				AND member.account_id=$2 AND member.status='active'
			WHERE m.id=$1 AND m.sender_account_id<>$2 AND m.removed_at IS NULL
			AND EXISTS(SELECT 1 FROM accounts WHERE id=m.sender_account_id AND status='active')
			AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE
				(b.blocker_account_id=$2 AND b.blocked_account_id=m.sender_account_id) OR
				(b.blocker_account_id=m.sender_account_id AND b.blocked_account_id=$2))`, input.TargetID, reporterID).Scan(&communityID)
		if errors.Is(err, pgx.ErrNoRows) {
			return safety.Report{}, safety.ErrTargetNotFound
		}
		if err != nil {
			return safety.Report{}, err
		}
		state, err := communityChatState(ctx, tx, reporterID, communityID)
		if errors.Is(err, communitychat.ErrNotFound) || (err == nil && !state.Joined) {
			return safety.Report{}, safety.ErrTargetNotFound
		}
		if err != nil {
			return safety.Report{}, err
		}
		communityMessageAllowed = true
	}
	activityMessageAllowed := false
	if input.TargetType == "activity_message" {
		var activityID string
		err := tx.QueryRow(ctx, `SELECT c.activity_id FROM activity_conversation_messages m
			JOIN activity_conversations c ON c.id=m.conversation_id
			JOIN activity_conversation_members member ON member.conversation_id=c.id
				AND member.account_id=$2 AND member.status='active'
			WHERE m.id=$1 AND m.sender_account_id<>$2 AND m.removed_at IS NULL
			AND EXISTS(SELECT 1 FROM accounts WHERE id=m.sender_account_id AND status='active')
			AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE
				(b.blocker_account_id=$2 AND b.blocked_account_id=m.sender_account_id) OR
				(b.blocker_account_id=m.sender_account_id AND b.blocked_account_id=$2))`, input.TargetID, reporterID).Scan(&activityID)
		if errors.Is(err, pgx.ErrNoRows) {
			return safety.Report{}, safety.ErrTargetNotFound
		}
		if err != nil {
			return safety.Report{}, err
		}
		state, err := chatState(ctx, tx, reporterID, activityID)
		if errors.Is(err, activitychat.ErrNotFound) || (err == nil && !state.Joined) {
			return safety.Report{}, safety.ErrTargetNotFound
		}
		if err != nil {
			return safety.Report{}, err
		}
		activityMessageAllowed = true
	}
	if input.TargetID != "" {
		targetID = input.TargetID
	}
	item, err := scanReport(tx.QueryRow(ctx, `INSERT INTO incident_reports
		(reporter_account_id,target_type,target_id,reason,details)
		SELECT $1,$2,$3,$4,$5 WHERE
		($2='general' OR ($2='activity_message' AND $6::boolean) OR ($2='community_message' AND $7::boolean) OR
		 ($2='activity' AND EXISTS (SELECT 1 FROM activities a WHERE a.id=$3 AND a.publication_status='published')) OR
		 ($2='organization' AND EXISTS (SELECT 1 FROM organizations o WHERE o.id=$3 AND o.status='active')) OR
		 ($2='account' AND EXISTS (SELECT 1 FROM accounts a WHERE a.id=$3 AND a.status='active')) OR
		 ($2='message' AND EXISTS (SELECT 1 FROM conversation_messages m
		   JOIN conversations c ON c.id=m.conversation_id
		   WHERE m.id=$3 AND m.sender_account_id<>$1
		   AND $1 IN (c.member_a_account_id,c.member_b_account_id))) OR
		 ($2='community' AND EXISTS (SELECT 1 FROM communities c WHERE c.id=$3
		   AND c.lifecycle_status='active' AND
		   ((c.publication_status='published' AND c.visibility<>'hidden') OR
		    EXISTS (SELECT 1 FROM community_memberships cm WHERE cm.community_id=c.id
		      AND cm.user_account_id=$1 AND cm.status='active')))) OR
		 ($2='business' AND EXISTS (SELECT 1 FROM businesses b
		   JOIN accounts principal ON principal.id=b.account_id
		   WHERE b.id=$3 AND b.status='active' AND b.claim_status='verified'
		   AND principal.status='active')))
		RETURNING `+reportColumns, reporterID, input.TargetType, targetID, input.Reason, input.Details, activityMessageAllowed, communityMessageAllowed))
	if errors.Is(err, pgx.ErrNoRows) {
		return safety.Report{}, safety.ErrTargetNotFound
	}
	if err != nil {
		return safety.Report{}, err
	}
	return item, nil
}

func (s *Store) ListOwnReports(ctx context.Context, reporterID string) ([]safety.Report, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+reportColumns+` FROM incident_reports
		WHERE reporter_account_id=$1 ORDER BY created_at DESC,id DESC LIMIT 50`, reporterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []safety.Report{}
	for rows.Next() {
		item, err := scanReport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

var _ safety.ReportControlStore = (*Store)(nil)

func reportControlQuota(ctx context.Context, f humanWriteFrame) (int, string, error) {
	var n int
	var source string
	e := f.tx.QueryRow(ctx, `SELECT count(*)::int,COALESCE(string_agg(concat_ws(':',id::text,xmin::text,created_at::text),',' ORDER BY id),'') FROM incident_reports WHERE reporter_account_id=$1 AND created_at>clock_timestamp()-interval '24 hours'`, f.a.ID).Scan(&n, &source)
	if e != nil {
		return 0, "", identity.ErrControlUnavailable
	}
	remaining := 5 - n
	if remaining < 0 {
		remaining = 0
	}
	return remaining, identity.HumanControlTextHash(source), nil
}

// Only exact subjects admitted by the original reporter policy are read. The
// server source proof includes current rows/ACL epochs, never subject bodies.
func reportControlTarget(ctx context.Context, f humanWriteFrame, in safety.ReportInput, lock bool) (string, *time.Time, error) {
	if in.TargetType == "general" {
		return "general", nil, nil
	}
	var query string
	var suffix string
	var source string
	var deadline *time.Time
	switch in.TargetType {
	case "account":
		query = `SELECT a.xmin::text,NULL::timestamptz FROM accounts a WHERE a.id=$1 AND a.status='active'`
		suffix = " FOR SHARE OF a"
	case "activity":
		query = `SELECT concat_ws(':',a.xmin::text,c.xmin::text,ao.xmin::text,birdtie_entity_cover_authority('activity',a.id,$2::uuid)),least(a.expires_at,c.expires_at)` + publishedActivityFrom + ` AND a.id=$1 AND(a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND(c.expires_at IS NULL OR c.expires_at>clock_timestamp())`
		suffix = " FOR SHARE OF a,c,ao"
	case "organization":
		query = `SELECT concat_ws(':',o.xmin::text,p.xmin::text,COALESCE((SELECT string_agg(m.xmin::text,',' ORDER BY m.user_account_id) FROM organization_memberships m WHERE m.organization_id=o.id AND m.user_account_id=$2),'')),NULL::timestamptz FROM organizations o JOIN accounts p ON p.id=o.account_id WHERE o.id=$1 AND o.status='active' AND p.status='active' AND(o.visibility='public' OR EXISTS(SELECT 1 FROM organization_memberships m WHERE m.organization_id=o.id AND m.user_account_id=$2 AND m.status='active'))`
		suffix = " FOR SHARE OF o,p"
	case "business":
		query = `SELECT concat_ws(':',b.xmin::text,p.xmin::text),NULL::timestamptz FROM businesses b JOIN accounts p ON p.id=b.account_id WHERE b.id=$1 AND b.status='active' AND b.claim_status='verified' AND p.status='active'`
		suffix = " FOR SHARE OF b,p"
	case "community":
		query = `SELECT concat_ws(':',c.xmin::text,COALESCE((SELECT string_agg(m.xmin::text,',' ORDER BY m.user_account_id) FROM community_memberships m WHERE m.community_id=c.id AND m.user_account_id=$2),'')),NULL::timestamptz FROM communities c WHERE c.id=$1 AND c.lifecycle_status='active' AND((c.publication_status='published' AND c.visibility<>'hidden') OR EXISTS(SELECT 1 FROM community_memberships m WHERE m.community_id=c.id AND m.user_account_id=$2 AND m.status='active'))`
		suffix = " FOR SHARE OF c"
	case "message":
		query = `SELECT concat_ws(':',m.xmin::text,c.xmin::text,p.xmin::text,COALESCE((SELECT string_agg(b.xmin::text,',' ORDER BY b.blocker_account_id) FROM account_blocks b WHERE(b.blocker_account_id=$2 AND b.blocked_account_id=m.sender_account_id)OR(b.blocker_account_id=m.sender_account_id AND b.blocked_account_id=$2)),'')),NULL::timestamptz FROM conversation_messages m JOIN conversations c ON c.id=m.conversation_id JOIN accounts p ON p.id=m.sender_account_id WHERE m.id=$1 AND m.sender_account_id<>$2 AND $2 IN(c.member_a_account_id,c.member_b_account_id) AND p.status='active'`
		suffix = " FOR SHARE OF m,c,p"
	case "community_message":
		query = `SELECT concat_ws(':',m.xmin::text,c.xmin::text,member.xmin::text,p.xmin::text),NULL::timestamptz FROM community_conversation_messages m JOIN community_conversations c ON c.id=m.conversation_id JOIN community_conversation_members member ON member.conversation_id=c.id AND member.account_id=$2 AND member.status='active' JOIN accounts p ON p.id=m.sender_account_id WHERE m.id=$1 AND m.sender_account_id<>$2 AND m.removed_at IS NULL AND p.status='active' AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE(b.blocker_account_id=$2 AND b.blocked_account_id=m.sender_account_id)OR(b.blocker_account_id=m.sender_account_id AND b.blocked_account_id=$2))`
		suffix = " FOR SHARE OF m,c,member,p"
	case "activity_message":
		query = `SELECT concat_ws(':',m.xmin::text,c.xmin::text,member.xmin::text,p.xmin::text),NULL::timestamptz FROM activity_conversation_messages m JOIN activity_conversations c ON c.id=m.conversation_id JOIN activity_conversation_members member ON member.conversation_id=c.id AND member.account_id=$2 AND member.status='active' JOIN accounts p ON p.id=m.sender_account_id WHERE m.id=$1 AND m.sender_account_id<>$2 AND m.removed_at IS NULL AND p.status='active' AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE(b.blocker_account_id=$2 AND b.blocked_account_id=m.sender_account_id)OR(b.blocker_account_id=m.sender_account_id AND b.blocked_account_id=$2))`
		suffix = " FOR SHARE OF m,c,member,p"
	default:
		return "", nil, identity.ErrControlInvalid
	}
	query += ` AND $2::uuid IS NOT NULL`
	if lock {
		query += suffix
	}
	e := f.tx.QueryRow(ctx, query, in.TargetID, f.a.ID).Scan(&source, &deadline)
	if errors.Is(e, pgx.ErrNoRows) {
		return "", nil, safety.ErrTargetNotFound
	}
	if e != nil {
		return "", nil, identity.ErrControlUnavailable
	}
	if in.TargetType == "community_message" {
		var id string
		if e = f.tx.QueryRow(ctx, `SELECT c.community_id FROM community_conversations c JOIN community_conversation_messages m ON m.conversation_id=c.id WHERE m.id=$1`, in.TargetID).Scan(&id); e != nil {
			return "", nil, identity.ErrControlUnavailable
		}
		state, e := communityChatState(ctx, f.tx, f.a.ID, id)
		if e != nil || !state.Joined {
			return "", nil, safety.ErrTargetNotFound
		}
		source += ":" + humanWriteHashJSON(state)
		var rows string
		if e = f.tx.QueryRow(ctx, `SELECT concat_ws(':',c.xmin::text,COALESCE((SELECT string_agg(m.xmin::text,',' ORDER BY m.user_account_id) FROM community_memberships m WHERE m.community_id=c.id AND m.user_account_id=$2),'')) FROM communities c WHERE c.id=$1`, id, f.a.ID).Scan(&rows); e != nil {
			return "", nil, identity.ErrControlUnavailable
		}
		source += ":" + rows
	}
	if in.TargetType == "activity_message" {
		var id string
		if e = f.tx.QueryRow(ctx, `SELECT c.activity_id FROM activity_conversations c JOIN activity_conversation_messages m ON m.conversation_id=c.id WHERE m.id=$1`, in.TargetID).Scan(&id); e != nil {
			return "", nil, identity.ErrControlUnavailable
		}
		state, e := chatState(ctx, f.tx, f.a.ID, id)
		if e != nil || !state.Joined {
			return "", nil, safety.ErrTargetNotFound
		}
		source += ":" + humanWriteHashJSON(state)
		var rows string
		if e = f.tx.QueryRow(ctx, `SELECT concat_ws(':',a.xmin::text,COALESCE((SELECT string_agg(p.xmin::text,',' ORDER BY p.participant_account_id) FROM activity_participations p WHERE p.activity_id=a.id AND p.participant_account_id=$2),'')) FROM activities a WHERE a.id=$1`, id, f.a.ID).Scan(&rows); e != nil {
			return "", nil, identity.ErrControlUnavailable
		}
		source += ":" + rows
	}
	if in.TargetType == "message" || in.TargetType == "community_message" || in.TargetType == "activity_message" {
		table := "conversation_messages"
		if in.TargetType == "community_message" {
			table = "community_conversation_messages"
		}
		if in.TargetType == "activity_message" {
			table = "activity_conversation_messages"
		}
		var sender string
		if e = f.tx.QueryRow(ctx, `SELECT sender_account_id FROM `+table+` WHERE id=$1`, in.TargetID).Scan(&sender); e != nil {
			return "", nil, identity.ErrControlUnavailable
		}
		if lock {
			first, second := f.a.ID+":BLOCKPAIR:"+sender, sender+":BLOCKPAIR:"+f.a.ID
			if first > second {
				first, second = second, first
			}
			if _, e = f.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,142)),pg_advisory_xact_lock(hashtextextended($2,142))`, first, second); e != nil {
				return "", nil, identity.ErrControlUnavailable
			}
		}
		var pairEpochs string
		if e = f.tx.QueryRow(ctx, `SELECT COALESCE(string_agg(concat_ws(':',owner_account_id::text,target_account_id::text,version::text,xmin::text),',' ORDER BY owner_account_id,target_account_id),'') FROM person_block_control_epochs WHERE(owner_account_id=$1 AND target_account_id=$2)OR(owner_account_id=$2 AND target_account_id=$1)`, f.a.ID, sender).Scan(&pairEpochs); e != nil {
			return "", nil, identity.ErrControlUnavailable
		}
		source += ":" + pairEpochs
	}
	return identity.HumanControlTextHash(source), deadline, nil
}
func reportControlQuotaDeadline(ctx context.Context, f humanWriteFrame) (*time.Time, error) {
	var until *time.Time
	if e := f.tx.QueryRow(ctx, `SELECT min(created_at+interval '24 hours') FROM incident_reports WHERE reporter_account_id=$1 AND created_at>clock_timestamp()-interval '24 hours'`, f.a.ID).Scan(&until); e != nil {
		return nil, identity.ErrControlUnavailable
	}
	return until, nil
}
func reportControlSource(ctx context.Context, f humanWriteFrame, in safety.ReportInput, lock bool) (string, string, *time.Time, error) {
	remaining, quota, e := reportControlQuota(ctx, f)
	if e != nil {
		return "", "", nil, e
	}
	if remaining <= 0 {
		return "", "", nil, safety.ErrRateLimited
	}
	target, deadline, e := reportControlTarget(ctx, f, in, lock)
	if e != nil {
		return "", "", nil, e
	}
	quotaDeadline, e := reportControlQuotaDeadline(ctx, f)
	if e != nil {
		return "", "", nil, e
	}
	if quotaDeadline != nil && (deadline == nil || quotaDeadline.Before(*deadline)) {
		deadline = quotaDeadline
	}
	return identity.HumanControlTextHash(quota + ":" + target), target, deadline, nil
}
func (s *Store) ReadReportControl(ctx context.Context, d [32]byte, a identity.Actor) (safety.ReportControlState, error) {
	ctx, f, e := s.humanWriteBegin(ctx, d, a, "REPORT", "")
	if e != nil {
		return safety.ReportControlState{}, e
	}
	defer f.tx.Rollback(context.Background())
	remaining, source, e := reportControlQuota(ctx, f)
	if e != nil {
		return safety.ReportControlState{}, e
	}
	sourceDeadline, e := reportControlQuotaDeadline(ctx, f)
	if e != nil {
		return safety.ReportControlState{}, e
	}
	now, until, e := s.humanWriteNow(ctx, f, sourceDeadline)
	if e != nil {
		return safety.ReportControlState{}, e
	}
	check := func(ctx context.Context, next humanWriteFrame) error {
		_, current, e := reportControlQuota(ctx, next)
		if e != nil {
			return e
		}
		if current != source {
			return identity.ErrControlChanged
		}
		return nil
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, until, func() error { return check(ctx, f) })); e != nil {
		return safety.ReportControlState{}, e
	}
	return safety.ReportControlState{RemainingQuota: remaining, CanSubmit: remaining > 0, ObservedAt: now, ExpiresAt: until, ResponseProof: humanWriteAttachProof(s, f, until, check)}, nil
}
func (s *Store) PreviewReportControl(ctx context.Context, d [32]byte, a identity.Actor, in safety.ReportControlInput) (safety.ReportControlPreview, error) {
	in, e := safety.NormalizeReportControl(in, false)
	if e != nil {
		return safety.ReportControlPreview{}, e
	}
	ctx, f, e := s.humanWriteBegin(ctx, d, a, "REPORT", in.OperationID)
	if e != nil {
		return safety.ReportControlPreview{}, e
	}
	defer f.tx.Rollback(context.Background())
	if _, e = humanWriteReceiptTx(ctx, f); e == nil {
		return safety.ReportControlPreview{}, identity.ErrControlChanged
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return safety.ReportControlPreview{}, identity.ErrControlUnavailable
	}
	source, _, deadline, e := reportControlSource(ctx, f, in.ReportInput, false)
	if e != nil {
		return safety.ReportControlPreview{}, e
	}
	base, e := s.humanWritePreview(ctx, f, "", in.Action, safety.ReportControlDigest(in), source, deadline)
	if e != nil {
		return safety.ReportControlPreview{}, e
	}
	check := func(ctx context.Context, next humanWriteFrame) error {
		current, _, _, e := reportControlSource(ctx, next, in.ReportInput, false)
		if e != nil {
			return e
		}
		if current != source {
			return identity.ErrControlChanged
		}
		return nil
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, base.ExpiresAt, func() error { return check(ctx, f) })); e != nil {
		return safety.ReportControlPreview{}, e
	}
	base.ResponseProof = humanWriteAttachProof(s, f, base.ExpiresAt, check)
	return safety.ReportControlPreview{HumanControlPreview: base, ReportInput: in.ReportInput}, nil
}

// Pair locks precede actor quota and all resource row locks, matching the
// canonical block writer. Read authorization here takes no row locks; the
// concrete source is checked again after the locks and before any effect.
func reportControlPairLock(ctx context.Context, f humanWriteFrame, in safety.ReportInput) error {
	if in.TargetType != "message" && in.TargetType != "community_message" && in.TargetType != "activity_message" {
		return nil
	}
	if _, _, e := reportControlTarget(ctx, f, in, false); e != nil {
		return e
	}
	table := "conversation_messages"
	if in.TargetType == "community_message" {
		table = "community_conversation_messages"
	}
	if in.TargetType == "activity_message" {
		table = "activity_conversation_messages"
	}
	var sender string
	if e := f.tx.QueryRow(ctx, `SELECT sender_account_id FROM `+table+` WHERE id=$1`, in.TargetID).Scan(&sender); e != nil {
		return identity.ErrControlUnavailable
	}
	first, second := f.a.ID+":BLOCKPAIR:"+sender, sender+":BLOCKPAIR:"+f.a.ID
	if first > second {
		first, second = second, first
	}
	if _, e := f.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,142)),pg_advisory_xact_lock(hashtextextended($2,142))`, first, second); e != nil {
		return identity.ErrControlUnavailable
	}
	return nil
}
func (s *Store) CommitReportControl(ctx context.Context, d [32]byte, a identity.Actor, in safety.ReportControlInput) (identity.HumanControlReceipt, error) {
	in, e := safety.NormalizeReportControl(in, true)
	if e != nil {
		return identity.HumanControlReceipt{}, e
	}
	ctx, f, e := s.humanWriteBegin(ctx, d, a, "REPORT", in.OperationID)
	if e != nil {
		return identity.HumanControlReceipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	digest := safety.ReportControlDigest(in)
	if r, done, e := s.humanWriteReplay(ctx, f, "", digest); done {
		return r, e
	}
	if e = reportControlPairLock(ctx, f, in.ReportInput); e != nil {
		return identity.HumanControlReceipt{}, e
	}
	var actor string
	if e = f.tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR UPDATE`, a.ID).Scan(&actor); e != nil {
		return identity.HumanControlReceipt{}, identity.ErrControlChanged
	}
	source, target, sourceDeadline, e := reportControlSource(ctx, f, in.ReportInput, true)
	if e != nil {
		return identity.HumanControlReceipt{}, e
	}
	deadline, e := s.humanWriteApprove(ctx, f, "", in.Action, digest, source, in.Approval, sourceDeadline)
	if e != nil {
		return identity.HumanControlReceipt{}, e
	}
	v, e := reportEffectTx(ctx, f.tx, a.ID, in.ReportInput)
	if e != nil {
		return identity.HumanControlReceipt{}, e
	}
	if _, e = auditExec(ctx, f.tx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose)VALUES($1,'submit','incident_report',$2,'allowed','personal_safety')`, a.ID, v.ID); e != nil {
		return identity.HumanControlReceipt{}, identity.ErrControlUnavailable
	}
	_, postQuota, e := reportControlQuota(ctx, f)
	if e != nil {
		return identity.HumanControlReceipt{}, e
	}
	r, e := humanWriteSaveReceipt(ctx, f, "", in.Action, in.TargetID, v.ID, digest, 0)
	if e != nil {
		return r, e
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, deadline, func() error {
		current, _, e := reportControlTarget(ctx, f, in.ReportInput, true)
		if e != nil {
			return e
		}
		_, quota, e := reportControlQuota(ctx, f)
		if e != nil {
			return e
		}
		if current != target || quota != postQuota {
			return identity.ErrControlChanged
		}
		return nil
	})); e != nil {
		return identity.HumanControlReceipt{}, e
	}
	return r, nil
}
func (s *Store) ReadReportControlOperation(ctx context.Context, d [32]byte, a identity.Actor, op string) (identity.HumanControlReceipt, error) {
	return s.humanWriteHistory(ctx, d, a, "REPORT", op, "", false)
}
func (s *Store) StopReportControlOperation(ctx context.Context, d [32]byte, a identity.Actor, op string) (identity.HumanControlReceipt, error) {
	return s.humanWriteHistory(ctx, d, a, "REPORT", op, "", true)
}
