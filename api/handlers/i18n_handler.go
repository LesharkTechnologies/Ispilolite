package handlers

import (
	"ispilolite/api/dto"
	"ispilolite/internal/i18n"
	"net/http"
)

func Translations(w http.ResponseWriter, r *http.Request) {
	locale := pathParam(r.URL.Path, "/api/v1/i18n/")
	respondWithJSON(w, http.StatusOK, dto.APIResponse{Success: true, Data: map[string]interface{}{"locale": i18n.Normalize(locale), "translations": i18n.Catalog(locale)}})
}
