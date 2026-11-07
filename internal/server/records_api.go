package server

import "net/http"

// ECG recordings and hearing tests from Apple Health. Like medications and
// symptoms they are the owner's alone: no link reaches them.

func (s *Server) handleListECGs(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListECGs(r.Context(), actorID(r.Context()), 500)
	if err != nil {
		s.fail(w, err, "ecgs")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetECG(w http.ResponseWriter, r *http.Request) {
	e, err := s.store.GetECG(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "ecg")
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) handleListAudiograms(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListAudiograms(r.Context(), actorID(r.Context()), 200)
	if err != nil {
		s.fail(w, err, "audiograms")
		return
	}
	writeJSON(w, http.StatusOK, list)
}
