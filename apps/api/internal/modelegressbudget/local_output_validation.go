package modelegressbudget

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

// Output failure is separate from transport retries. Only the actual driver
// after receipt and native settlement constructs it. No raw body is retained.
type LocalOutputFailure struct {
	operation string
	request   modelgateway.Request
	hash      [32]byte
	code      string
}

func newLocalOutputFailure(op string, req modelgateway.Request, code string) *LocalOutputFailure {
	if op == "" || (code != "INVALID_JSON" && code != "TRUNCATED") || modelgateway.ValidateRequest(req, time.Now()) != nil || req.OutputMode != modelgateway.Structured {
		return nil
	}
	raw, e := json.Marshal(req)
	if e != nil {
		return nil
	}
	var copied modelgateway.Request
	if json.Unmarshal(raw, &copied) != nil {
		return nil
	}
	return &LocalOutputFailure{operation: op, request: copied, hash: sha256.Sum256(raw), code: code}
}

func (f LocalOutputFailure) OperationRequest() (string, modelgateway.Request, bool) {
	raw, e := json.Marshal(f.request)
	if e != nil || f.operation == "" || (f.code != "INVALID_JSON" && f.code != "TRUNCATED") || sha256.Sum256(raw) != f.hash {
		return "", modelgateway.Request{}, false
	}
	var copied modelgateway.Request
	if json.Unmarshal(raw, &copied) != nil {
		return "", modelgateway.Request{}, false
	}
	return f.operation, copied, true
}

func (LocalOutputFailure) MarshalJSON() ([]byte, error) { return nil, ErrServerOnly }
func (f *LocalOutputFailure) UnmarshalJSON([]byte) error {
	*f = LocalOutputFailure{}
	return ErrServerOnly
}

type LocalOutputPort interface {
	CheckOwnLocalOutputFailure(context.Context, agentevent.Access, LocalRetryProof, LocalOutputFailure, *agentfeature.Controller) (LocalReleaseCheckpoint, error)
}
