package handlers

import (
	"net/http"

	"deepseek2api-go/internal/state"
)

func SyncStatus(st *state.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		WriteJSON(w, http.StatusOK, st.SyncStatusSnapshot())
	}
}
