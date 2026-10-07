package postgres

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
)

// currentEgressQuery selects only the current scalar user question from the
// native Task snapshot. History is checked for consistency and never exported.
// The original source generation and authority guards still surround this read.
func currentEgressQuery(original string, filtersJSON, conversationJSON []byte) (string, error) {
	var filters map[string]string
	if len(filtersJSON) > 0 && json.Unmarshal(filtersJSON, &filters) != nil {
		return "", modelegressbudget.ErrDenied
	}
	var conversation []agentworkspace.Message
	if len(conversationJSON) > 0 && json.Unmarshal(conversationJSON, &conversation) != nil {
		return "", modelegressbudget.ErrDenied
	}
	valid := func(value string) bool {
		return strings.TrimSpace(value) != "" && len(value) <= 4096 && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
	}
	if !valid(original) {
		return "", modelegressbudget.ErrDenied
	}
	query := original
	current, hasCurrent := filters["currentQuery"]
	if hasCurrent {
		if !valid(current) {
			return "", modelegressbudget.ErrDenied
		}
		query = current
	}
	lastUser := ""
	for _, message := range conversation {
		if message.Role == "user" {
			if !valid(message.Text) {
				return "", modelegressbudget.ErrDenied
			}
			lastUser = message.Text
		}
	}
	if (hasCurrent && lastUser == "") || (lastUser != "" && strings.TrimSpace(lastUser) != strings.TrimSpace(query)) {
		return "", modelegressbudget.ErrDenied
	}
	return query, nil
}
