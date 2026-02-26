package handlers

import (
	"net/http"

	"deepseek2api-go/internal/services"
)

func OpenAIModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	services.WriteOpenAIModels(w)
}
