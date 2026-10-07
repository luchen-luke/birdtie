package postgres

import (
	"encoding/json"
	"testing"
	"time"

	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
)

// Reuse the existing private seal and original native approval. No second
// grant, cache epoch or source ledger is introduced by AIR023.
func TestContextAuthoritySnapshotExistingPurposeSealAndPolicy(t *testing.T) {
	for _, name := range []string{"normalIdle", "policyABA", "grantRevoked", "otherOwner", "otherService", "forgedWire"} {
		t.Run(name, func(t *testing.T) {
			f := contextBuilderNative(t)
			b := f.place.private.base
			request := contextBuilderPurposeRequest(t, f)
			service, err := acb.NewService(b.store)
			if err != nil {
				t.Fatal(err)
			}
			built, err := service.Build(b.ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			access := request.Access
			switch name {
			case "normalIdle":
				if _, err = b.store.Authenticate(b.ctx, access.SessionDigest); err != nil {
					t.Fatal(err)
				}
			case "policyABA":
				expiry := f.now.Add(time.Hour)
				in := policyNativeInput(agentpolicysettings.Autonomy, built.Bundle.Policies[0].NativeRevision, expiry)
				in.Settings = json.RawMessage(`{"level":"LEVEL_1_ASSIST"}`)
				changed, e := b.store.PutOwnPolicy(b.ctx, access, agentpolicysettings.Autonomy, in)
				if e != nil {
					t.Fatal(e)
				}
				in = policyNativeInput(agentpolicysettings.Autonomy, changed.Autonomy.NativeRevision, expiry)
				if _, err = b.store.PutOwnPolicy(b.ctx, access, agentpolicysettings.Autonomy, in); err != nil {
					t.Fatal(err)
				}
			case "grantRevoked":
				if _, err = b.store.RevokeOwnContextPurpose(b.ctx, access, request.PurposeGrantID, 1); err != nil {
					t.Fatal(err)
				}
			case "otherOwner":
				access = f.place.private.peer
			case "otherService":
				service, err = acb.NewService(New(b.pool, false))
				if err != nil {
					t.Fatal(err)
				}
			case "forgedWire":
				if _, err = json.Marshal(built); err == nil {
					t.Fatal("server sealed context became wire permission")
				}
				built.Bundle.CurrentQuery = "客户端伪造的同名主体"
			}
			current, err := service.RevalidateOwn(b.ctx, access, built)
			if name == "normalIdle" {
				if err != nil || !current.Bundle.ExpiresAt.Equal(built.Bundle.ExpiresAt) || current.Bundle.ModelAccess != "UNAVAILABLE" {
					t.Fatal("normal idle extended or denied original snapshot", err)
				}
			} else {
				contextBuilderRequireEmpty(t, current, err)
			}
		})
	}
}
