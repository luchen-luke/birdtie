package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/jackc/pgx/v5"
)

var _ agentworkspace.NowPreferencePort = (*Store)(nil)

// All writes remain ordinary human self-management of the original native
// Memory. No candidate, inference, external grant or parallel preference store
// is created. The original Task receipt and Memory commit atomically.
func (s *Store) ExecuteOwnNowPreference(ctx context.Context, a arp.Access) (agentworkspace.Task, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil || !a.Valid() || !egressUUID(a.Actor.ID) || !egressUUID(a.TaskID) {
		return agentworkspace.Task{}, arp.ErrDenied
	}
	owner := actorref.PrincipalRef{Type: actorref.Person, ID: a.Actor.ID}
	access := agentprofile.PrivateAccess{SessionDigest: a.SessionDigest, WorkspacePrincipal: owner}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return agentworkspace.Task{}, arp.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return agentworkspace.Task{}, arp.ErrUnavailable
	}
	binding, e := lockOwnAgentPrivateBinding(ctx, tx, access, s.devPhoneEnabled)
	if e != nil {
		return agentworkspace.Task{}, memoryError(e)
	}
	if _, e = lockAgentPrivateMetadata(ctx, tx, binding, true); e != nil {
		return agentworkspace.Task{}, memoryError(e)
	}
	task, e := ownReplyTaskTx(ctx, tx, a, true)
	if e != nil {
		return task, e
	}
	if task.Intent == agentworkspace.NowPreferenceIntent && task.Status == agentworkspace.TaskCompleted && len(task.Conversation) > 1 && task.Conversation[len(task.Conversation)-1].Role == "assistant" {
		if e = recheckAgentPrivateSession(ctx, tx, binding); e != nil {
			return agentworkspace.Task{}, memoryError(e)
		}
		if e = tx.Commit(ctx); e != nil {
			return agentworkspace.Task{}, arp.ErrUnavailable
		}
		return task, nil
	}
	command, declarationIndex, authorizationIndex, e := agentworkspace.ResolveNowPreferenceCommand(task)
	if e != nil {
		return agentworkspace.Task{}, agentmemory.ErrInvalid
	}
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return agentworkspace.Task{}, arp.ErrUnavailable
	}
	message := "当前只支持本人明确声明的海鲜和辣味偏好；不会从朋友、临时要求或网页内容推断你的长期偏好。"
	trace := []string{}
	switch command.Action {
	case agentworkspace.NowPreferenceRemember:
		if !command.LocalUse {
			message = agentworkspace.NowPreferenceConfirmationText(command)
			break
		}
		fallthrough
	case agentworkspace.NowPreferenceCorrect:
		written := []string{}
		for _, item := range command.Items {
			current, missing, e := nowPreferenceCurrentTx(ctx, tx, binding, item.Topic)
			if e != nil {
				return agentworkspace.Task{}, e
			}
			var old agentworkspace.NowPreferenceValue
			if !missing {
				old, e = agentworkspace.ParseOwnStoredNowPreferenceMemory(current)
				if e != nil {
					return agentworkspace.Task{}, agentmemory.ErrConflict
				}
				if e = validateNowPreferenceSourcesTx(ctx, tx, binding, old); e != nil {
					return agentworkspace.Task{}, e
				}
			}
			if command.Action == agentworkspace.NowPreferenceCorrect && missing {
				continue
			}
			if command.Action == agentworkspace.NowPreferenceCorrect && (current.Status != agentmemory.StatusActive || !current.ValidUntil.After(now)) {
				return agentworkspace.Task{}, agentmemory.ErrConflict
			}
			id, expected := current.ID, current.Version
			if missing {
				id, e = humanCandidateUUID()
				if e != nil {
					return agentworkspace.Task{}, arp.ErrUnavailable
				}
				expected = 0
			}
			declaration, e := agentworkspace.NowPreferenceSourceForTask(task, declarationIndex)
			if e != nil {
				return agentworkspace.Task{}, agentmemory.ErrInvalid
			}
			authorization := old.Authorization
			if command.LocalUse {
				authorization, e = agentworkspace.NowPreferenceSourceForTask(task, authorizationIndex)
				if e != nil {
					return agentworkspace.Task{}, agentmemory.ErrInvalid
				}
			}
			value := agentworkspace.NowPreferenceValue{Schema: agentworkspace.NowPreferenceSchema, Nature: agentworkspace.NowPreferenceNature, Topic: item.Topic, Value: item.Value, UsePurpose: agentworkspace.NowPreferencePurpose, ConsentVersion: 1, Declaration: declaration, Authorization: authorization}
			raw, e := json.Marshal(value)
			if e != nil {
				return agentworkspace.Task{}, arp.ErrUnavailable
			}
			until := now.Add(agentmemory.MaxValidity).Truncate(time.Microsecond)
			if !command.LocalUse {
				until = current.ValidUntil
			} // A correction never silently renews an old use period.
			input := agentmemory.PutInput{ExpectedVersion: expected, MemoryType: agentmemory.TypePreference, MemoryKey: agentworkspace.NowPreferenceKey(item.Topic), Summary: agentworkspace.NowPreferenceSummary(item), StructuredValue: raw, Visibility: agentmemory.VisibilityPrivate, ValidUntil: until}
			memory, _, e := putOwnMemoryInTx(ctx, tx, binding, owner, id, input)
			if e != nil {
				return agentworkspace.Task{}, e
			}
			trace = append(trace, fmt.Sprintf("%s:%d", memory.ID, memory.Version))
			written = append(written, memory.Summary)
		}
		if len(written) == 0 {
			message = "没有找到可纠正的已授权口味偏好；如需保存，请明确说记住并用于以后本地推荐。"
		} else {
			message = "已记住：" + strings.Join(written, "；") + "。会用于以后的本人本地推荐，不发送给联网搜索或模型。你可以随时查看、纠正或忘记。"
		}
	case agentworkspace.NowPreferenceForget:
		deleted := 0
		for _, item := range command.Items {
			current, missing, e := nowPreferenceCurrentTx(ctx, tx, binding, item.Topic)
			if e != nil {
				return agentworkspace.Task{}, e
			}
			if missing {
				continue
			}
			value, e := agentworkspace.ParseOwnStoredNowPreferenceMemory(current)
			if e != nil {
				return agentworkspace.Task{}, agentmemory.ErrConflict
			}
			if e = validateNowPreferenceSourcesTx(ctx, tx, binding, value); e != nil {
				return agentworkspace.Task{}, e
			}
			memory, e := deleteOwnMemoryInTx(ctx, tx, binding, current.ID, current.Version)
			if e != nil {
				return agentworkspace.Task{}, e
			}
			trace = append(trace, fmt.Sprintf("%s:%d", memory.ID, memory.Version))
			deleted++
		}
		if deleted == 0 {
			message = "当前没有这些可使用的口味偏好。"
		} else {
			message = fmt.Sprintf("已忘记 %d 项口味偏好；下一次本人本地推荐将停止使用。", deleted)
		}
	case agentworkspace.NowPreferenceView:
		summaries := []string{}
		for _, topic := range []string{"seafood", "spicy"} {
			current, missing, e := nowPreferenceCurrentTx(ctx, tx, binding, topic)
			if e != nil {
				return agentworkspace.Task{}, e
			}
			if missing {
				continue
			}
			value, e := agentworkspace.ParseNowPreferenceMemory(current, now)
			if e != nil {
				continue
			}
			if e = validateNowPreferenceSourcesTx(ctx, tx, binding, value); e != nil {
				return agentworkspace.Task{}, e
			}
			summaries = append(summaries, current.Summary)
			trace = append(trace, fmt.Sprintf("%s:%d", current.ID, current.Version))
		}
		if len(summaries) == 0 {
			message = "当前没有已授权用于本人本地推荐的海鲜或辣味偏好。"
		} else {
			message = "当前记住：" + strings.Join(summaries, "；") + "。仅用于本人本地推荐；可以说“忘记我的口味偏好”删除。"
		}
	}
	filters := map[string]string{}
	for k, v := range task.Filters {
		filters[k] = v
	}
	task.Filters = filters
	agentworkspace.ClearPlaceFollowupContext(&task)
	task.Intent, task.Status = agentworkspace.NowPreferenceIntent, agentworkspace.TaskCompleted
	task.Filters["currentQuery"] = task.Conversation[len(task.Conversation)-1].Text
	task.Filters["nowPreferenceTrace"] = strings.Join(trace, ",")
	task.Filters["nowPreferenceAction"] = command.Action
	task.Conversation = append(task.Conversation, agentworkspace.Message{Role: "assistant", Text: message})
	task, e = s.updateTaskInTx(ctx, tx, task)
	if e != nil {
		return agentworkspace.Task{}, arp.ErrUnavailable
	}
	// Last fence observes the exact post-write original Task and still-current
	// session after all native resource locks, audits and outbox triggers.
	raw, e := json.Marshal(agentworkspace.SanitizeTaskForResponse(task))
	if e != nil {
		return agentworkspace.Task{}, arp.ErrUnavailable
	}
	a.ExpectedTask = raw
	if _, e = ownReplyTaskTx(ctx, tx, a, false); e != nil {
		return agentworkspace.Task{}, e
	}
	if e = recheckAgentPrivateSession(ctx, tx, binding); e != nil {
		return agentworkspace.Task{}, memoryError(e)
	}
	if ctx.Err() != nil {
		return agentworkspace.Task{}, arp.ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return agentworkspace.Task{}, arp.ErrUnavailable
	}
	return task, nil
}

func nowPreferenceCurrentTx(ctx context.Context, tx pgx.Tx, binding agentPrivateBinding, topic string) (agentmemory.Record, bool, error) {
	key := agentworkspace.NowPreferenceKey(topic)
	if key == "" {
		return agentmemory.Record{}, false, agentmemory.ErrInvalid
	}
	m, e := scanAgentMemory(tx.QueryRow(ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories WHERE agent_id=$1 AND owner_id=$2 AND owner_type='PERSON' AND memory_type='PREFERENCE' AND memory_key=$3 AND source_type='EXPLICIT' AND status IN ('ACTIVE','EXPIRED') FOR UPDATE`, binding.agentID, binding.accountID, key))
	if errors.Is(e, pgx.ErrNoRows) {
		return agentmemory.Record{}, true, nil
	}
	if e != nil {
		return agentmemory.Record{}, false, agentmemory.ErrUnavailable
	}
	return m, false, nil
}

// Shared by the future local Context Builder reader. Task IDs/digests are only
// correlation; both retained native user statements must still belong to this
// current owner. No transcript or Memory leaves the native database boundary.
func validateNowPreferenceSourcesTx(ctx context.Context, tx pgx.Tx, binding agentPrivateBinding, value agentworkspace.NowPreferenceValue) error {
	read := func(id string) (agentworkspace.Task, error) {
		t, e := scanAgentTask(tx.QueryRow(ctx, `SELECT `+agentTaskColumns+` FROM agent_tasks WHERE id=$1 AND owner_account_id=$2 AND acting_user_account_id=$2 AND principal_type='person' FOR SHARE`, id, binding.accountID))
		if e != nil {
			return agentworkspace.Task{}, agentmemory.ErrForbidden
		}
		return t, nil
	}
	declaration, e := read(value.Declaration.TaskID)
	if e != nil {
		return e
	}
	authorization := declaration
	if value.Authorization.TaskID != value.Declaration.TaskID {
		authorization, e = read(value.Authorization.TaskID)
		if e != nil {
			return e
		}
	}
	if e = agentworkspace.ValidateNowPreferenceSources(value, declaration, authorization); e != nil {
		return agentmemory.ErrForbidden
	}
	return nil
}
