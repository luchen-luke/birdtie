package saved

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

const MaxNoteRevision int64 = 9007199254740991
const MaxNoteBytes = 2000

var (
	ErrInvalidNote     = errors.New("invalid saved note")
	ErrNoteChanged     = errors.New("saved note changed")
	ErrNoteUnavailable = errors.New("saved note unavailable")
	noteUUID           = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

type Note struct {
	SavedID  string `json:"savedId"`
	Note     string `json:"note"`
	Revision int64  `json:"revision"`
}

type NoteInput struct {
	Note             string `json:"note"`
	ExpectedRevision int64  `json:"expectedRevision"`
	OperationID      string `json:"operationId"`
}

// Receipts retain the exact operation and its result version, never note text.
// A newer current note does not erase the receipt of an earlier operation.
type NoteOperation struct {
	SavedID     string `json:"savedId"`
	OperationID string `json:"operationId"`
	Revision    int64  `json:"revision"`
	Status      string `json:"status"`
}

type HumanNoteStore interface {
	GetHumanSavedNote(context.Context, [32]byte, identity.Actor, string) (Note, error)
	UpdateHumanSavedNote(context.Context, [32]byte, identity.Actor, string, NoteInput) (NoteOperation, error)
	GetHumanSavedNoteOperation(context.Context, [32]byte, identity.Actor, string, string) (NoteOperation, error)
}

func ValidNoteID(id string) bool {
	return noteUUID.MatchString(id) && id != "00000000-0000-0000-0000-000000000000"
}

func NormalizeNoteInput(in NoteInput) (NoteInput, error) {
	if !utf8.ValidString(in.Note) || strings.ContainsRune(in.Note, '\x00') || len(in.Note) > MaxNoteBytes || in.ExpectedRevision < 0 || in.ExpectedRevision >= MaxNoteRevision || !ValidNoteID(in.OperationID) {
		return NoteInput{}, ErrInvalidNote
	}
	in.Note = strings.TrimSpace(in.Note)
	in.OperationID = strings.ToLower(in.OperationID)
	return in, nil
}

func NoteInputDigest(savedID string, in NoteInput) [32]byte {
	value := struct {
		SavedID          string `json:"savedId"`
		Note             string `json:"note"`
		ExpectedRevision int64  `json:"expectedRevision"`
	}{strings.ToLower(savedID), in.Note, in.ExpectedRevision}
	raw, _ := json.Marshal(value)
	return sha256.Sum256(raw)
}
