package handlers

import (
	"net/http"
	"strconv"

	"ispilolite/internal/services/matching"
)

type MatchingHandler struct{ service *matching.MatchingService }
func NewMatchingHandler(service *matching.MatchingService)*MatchingHandler{return &MatchingHandler{service:service}}
func (h *MatchingHandler) Match(w http.ResponseWriter,r *http.Request){id:=pathParam(r.URL.Path,"/api/v1/matches/request/");limit,_:=strconv.Atoi(r.URL.Query().Get("limit"));if limit<=0{limit=10};items,err:=h.service.MatchProviders(r.Context(),id,matching.MatchOptions{RadiusKM:10});if err!=nil{respondWithError(w,500,err.Error());return};if len(items)>limit{items=items[:limit]};respondWithJSON(w,200,map[string]interface{}{"success":true,"data":items})}
