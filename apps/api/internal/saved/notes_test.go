package saved

import (
	"errors"
	"strings"
	"testing"
)

const noteTestID = "11111111-1111-4111-8111-111111111111"
const noteTestOp = "22222222-2222-4222-8222-222222222222"

func TestPrivateSavedNoteInputBoundaries(t *testing.T) {
	for _, note := range []string{"", "  私人备注\nSecond line  ", strings.Repeat("字", 666) + "aa"} {
		in, e := NormalizeNoteInput(NoteInput{Note: note, ExpectedRevision: 0, OperationID: noteTestOp})
		if e != nil || in.Note != strings.TrimSpace(note) {
			t.Fatal("valid private annotation rejected", e)
		}
	}
	for _, in := range []NoteInput{
		{Note: string([]byte{0xff}), OperationID: noteTestOp},
		{Note: "before\x00after", OperationID: noteTestOp},
		{Note: strings.Repeat("字", 667), OperationID: noteTestOp},
		{ExpectedRevision: -1, OperationID: noteTestOp},
		{ExpectedRevision: MaxNoteRevision, OperationID: noteTestOp},
		{OperationID: "00000000-0000-0000-0000-000000000000"},
		{OperationID: "invalid"},
	} {
		if _, e := NormalizeNoteInput(in); !errors.Is(e, ErrInvalidNote) {
			t.Fatal("invalid input accepted", e)
		}
	}
	if _, e := NormalizeNoteInput(NoteInput{ExpectedRevision: MaxNoteRevision - 1, OperationID: noteTestOp}); e != nil {
		t.Fatal("last safe CAS revision rejected", e)
	}
}

func TestPrivateSavedNoteDigestBindsBookmarkBodyAndCAS(t *testing.T) {
	in := NoteInput{Note: "same", ExpectedRevision: 1, OperationID: noteTestOp}
	original := NoteInputDigest(noteTestID, in)
	for _, other := range []struct {
		id string
		in NoteInput
	}{
		{noteTestOp, in},
		{noteTestID, NoteInput{Note: "different", ExpectedRevision: 1}},
		{noteTestID, NoteInput{Note: "same", ExpectedRevision: 2}},
	} {
		if NoteInputDigest(other.id, other.in) == original {
			t.Fatal("operation digest lost a reviewed binding")
		}
	}
	in.OperationID = noteTestID
	if NoteInputDigest(noteTestID, in) != original {
		t.Fatal("receipt ID unexpectedly changed content digest")
	}
}
