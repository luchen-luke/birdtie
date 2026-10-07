package agentmemory

import "github.com/birdtie/birdtie/apps/api/internal/agentevent"

// DecodeEvidenceReferenceInput permits three addresses/expectations only.
// Reuse the native raw-body, duplicate-key and UTF-8 bounds; every supplied
// authorization, source version, status, score or observation claim is rejected.
func DecodeEvidenceReferenceInput(raw []byte) (EvidenceReferenceInput, error) {
	object, err := decodeObject(raw, 2, 4)
	if err != nil || len(object) != 3 {
		return EvidenceReferenceInput{}, ErrInvalid
	}
	expected, err := integerField(object, "expectedMemoryVersion")
	if err != nil {
		return EvidenceReferenceInput{}, ErrInvalid
	}
	sourceType, err := stringField(object, "sourceType")
	if err != nil {
		return EvidenceReferenceInput{}, ErrInvalid
	}
	id, err := stringField(object, "sourceId")
	if err != nil {
		return EvidenceReferenceInput{}, ErrInvalid
	}
	return NormalizeReference(EvidenceReferenceInput{ExpectedMemoryVersion: expected,
		SourceType: agentevent.SourceType(sourceType), SourceID: id})
}

func DecodeEvidenceDetachInput(raw []byte) (EvidenceDetachInput, error) {
	object, err := decodeObject(raw, 2, 2)
	if err != nil || len(object) != 1 {
		return EvidenceDetachInput{}, ErrInvalid
	}
	expected, err := integerField(object, "expectedVersion")
	if err != nil {
		return EvidenceDetachInput{}, ErrInvalid
	}
	return NormalizeEvidenceDetach(EvidenceDetachInput{ExpectedVersion: expected})
}

func (input *EvidenceReferenceInput) UnmarshalJSON(raw []byte) error {
	if input == nil {
		return ErrInvalid
	}
	decoded, err := DecodeEvidenceReferenceInput(raw)
	if err != nil {
		*input = EvidenceReferenceInput{}
		return ErrInvalid
	}
	*input = decoded
	return nil
}

func (input *EvidenceDetachInput) UnmarshalJSON(raw []byte) error {
	if input == nil {
		return ErrInvalid
	}
	decoded, err := DecodeEvidenceDetachInput(raw)
	if err != nil {
		*input = EvidenceDetachInput{}
		return ErrInvalid
	}
	*input = decoded
	return nil
}
