package httpapi

import "net/http"

func (s *server) readOrganizationMapControl(w http.ResponseWriter, r *http.Request) {
	a, p, id, ok := s.orgControlAccess(w, r)
	if !ok || !orgControlNoBody(w, r) {
		return
	}
	out, e := p.ReadMapControl(r.Context(), a, id)
	if e != nil {
		orgControlFailure(w, e)
		return
	}
	orgControlRespond(w, r, p, a, id, out.Proof, out.ExpiresAt, out)
}
