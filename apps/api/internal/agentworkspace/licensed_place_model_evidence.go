package agentworkspace

import (
	"errors"
	"time"
)

const LicensedPlaceModelReplySchema = "birdtie.licensed-place-model-reply.v1"

var ErrLicensedPlaceFactsUnavailable = errors.New("licensed public place facts unavailable for model answer")

// Correlation with the original standalone native model operation. These
// persisted selectors grant neither dispatch nor new reading authority.
// SourceRunID is deliberately absent: an operation is not an 088 ModelRun.
type LicensedPlaceModelEvidence struct {
	Schema               string              `json:"schema"`
	OperationID          string              `json:"operationId"`
	PreviewID            string              `json:"previewId"`
	BindingID            string              `json:"bindingId"`
	ProviderRequestID    string              `json:"providerRequestId"`
	Provider             string              `json:"provider"`
	Model                string              `json:"model"`
	RequestDigest        string              `json:"requestDigest"`
	PayloadDigest        string              `json:"payloadDigest"`
	CurrentQueryDigest   string              `json:"currentQueryDigest"`
	TextDigest           string              `json:"textDigest"`
	SourceEvidenceDigest string              `json:"sourceEvidenceDigest"`
	Facts                []LocalPlaceFactRef `json:"facts"`
	ValidUntil           time.Time           `json:"validUntil"`
	SearchCalls          int                 `json:"searchCalls"`
	ModelCalls           int                 `json:"modelCalls"`
}
