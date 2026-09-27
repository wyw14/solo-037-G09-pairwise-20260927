package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/wyw14/solo-037/internal/model"
	"github.com/wyw14/solo-037/internal/service"
)

type GroupService interface {
	TrialGroups() []service.TrialGroup
	CreateTrialGroup(name string, runIDs []model.RunID) (service.TrialGroup, error)
	TrialGroup(model.GroupID) (service.TrialGroup, bool)
	GroupConsistency(model.GroupID) (service.ConsistencyReport, error)
}

type createTrialGroupRequest struct {
	Name   string        `json:"name"`
	RunIDs []model.RunID `json:"run_ids"`
}

func (r *Router) trialGroups(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, r.dependencies.TrialGroups())
}

func (r *Router) createTrialGroup(w http.ResponseWriter, request *http.Request) {
	var input createTrialGroupRequest
	if err := decodeMeasurementBody(request, &input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	group, err := r.dependencies.CreateTrialGroup(input.Name, input.RunIDs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, group)
}

func (r *Router) trialGroup(w http.ResponseWriter, request *http.Request) {
	group, ok := r.dependencies.TrialGroup(model.GroupID(chi.URLParam(request, "id")))
	if !ok {
		http.Error(w, "trial group not found", http.StatusNotFound)
		return
	}
	writeJSON(w, group)
}

func (r *Router) groupConsistency(w http.ResponseWriter, request *http.Request) {
	// The endpoint returns 200 even when the runs are not comparable: blockers
	// are data in the body, not a request error. Only a missing group is 404.
	report, err := r.dependencies.GroupConsistency(model.GroupID(chi.URLParam(request, "id")))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, report)
}
