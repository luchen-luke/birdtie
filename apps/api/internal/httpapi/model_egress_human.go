package httpapi

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"net/http"
)

type humanModelEgressReadStore interface {
	ListOwnModelEgressOptions(context.Context, agentevent.Access) (modelegressbudget.HumanOptions, error)
	ListOwnModelEgressReceipts(context.Context, agentevent.Access) ([]modelegressbudget.HumanReceipt, error)
	ReadOwnModelEgressReceipt(context.Context, agentevent.Access, string) (modelegressbudget.HumanReceipt, error)
}
type humanEgressReceiptWire struct {
	modelegressbudget.HumanReceipt
	Review *humanModelEgressPreview `json:"review,omitempty"`
}

func humanReceiptWire(r modelegressbudget.HumanReceipt) (humanEgressReceiptWire, error) {
	out := humanEgressReceiptWire{HumanReceipt: r}
	if r.Preview != nil {
		d, e := humanEgressDisplay(*r.Preview)
		if e != nil {
			return out, e
		}
		out.Review = &d
	}
	return out, nil
}
func (s *server) humanEgressReadAccess(w http.ResponseWriter, r *http.Request) (agentevent.Access, humanModelEgressReadStore, bool) {
	a, _, ok := s.humanModelEgressAccess(w, r)
	if !ok {
		return a, nil, false
	}
	if !contextPurposeNoBody(w, r) {
		return a, nil, false
	}
	port, ok := s.catalog.(humanModelEgressReadStore)
	if !ok || port == nil {
		modelEgressHTTPFailure(w, modelegressbudget.ErrUnavailable)
		return a, nil, false
	}
	return a, port, true
}
func (s *server) listModelEgressOptions(w http.ResponseWriter, r *http.Request) {
	a, p, ok := s.humanEgressReadAccess(w, r)
	if !ok {
		return
	}
	d, e := p.ListOwnModelEgressOptions(r.Context(), a)
	if e != nil {
		modelEgressHTTPFailure(w, e)
		return
	}
	respond(w, 200, map[string]any{"data": d})
}
func (s *server) listModelEgressReceipts(w http.ResponseWriter, r *http.Request) {
	a, p, ok := s.humanEgressReadAccess(w, r)
	if !ok {
		return
	}
	items, e := p.ListOwnModelEgressReceipts(r.Context(), a)
	if e != nil {
		modelEgressHTTPFailure(w, e)
		return
	}
	out := make([]humanEgressReceiptWire, 0, len(items))
	for _, item := range items {
		d, err := humanReceiptWire(item)
		if err != nil {
			modelEgressHTTPFailure(w, err)
			return
		}
		out = append(out, d)
	}
	respond(w, 200, map[string]any{"data": out, "modelAccess": "UNAVAILABLE"})
}
func (s *server) readModelEgressReceipt(w http.ResponseWriter, r *http.Request) {
	a, p, ok := s.humanEgressReadAccess(w, r)
	if !ok {
		return
	}
	id := r.PathValue("previewID")
	if !uuidPath.MatchString(id) {
		modelEgressHTTPFailure(w, modelegressbudget.ErrInvalid)
		return
	}
	item, e := p.ReadOwnModelEgressReceipt(r.Context(), a, id)
	if e != nil {
		modelEgressHTTPFailure(w, e)
		return
	}
	d, e := humanReceiptWire(item)
	if e != nil {
		modelEgressHTTPFailure(w, e)
		return
	}
	respond(w, 200, map[string]any{"data": d})
}
