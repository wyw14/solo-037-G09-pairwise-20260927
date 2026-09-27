package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/wyw14/solo-037/internal/model"
	"github.com/wyw14/solo-037/internal/service"
)

type ComparisonService interface {
	CreateComparisonGroup(name string, runIDs []model.RunID) (service.ComparisonGroup, error)
	UpdateComparisonGroup(model.GroupID, string, []model.RunID) (service.ComparisonGroup, error)
	ComparisonGroups() []service.ComparisonGroup
	ComparisonGroup(model.GroupID) (service.ComparisonGroup, bool)
	AnalyzeComparisonGroup(model.GroupID) (service.GroupAnalysis, error)
}

type comparisonGroupRequest struct {
	Name   string        `json:"name"`
	RunIDs []model.RunID `json:"run_ids"`
}

func (r *Router) comparisonGroups(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, r.dependencies.ComparisonGroups())
}

func (r *Router) createComparisonGroup(w http.ResponseWriter, request *http.Request) {
	var input comparisonGroupRequest
	if err := decodeMeasurementBody(request, &input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	value, err := r.dependencies.CreateComparisonGroup(input.Name, input.RunIDs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, value)
}

func (r *Router) comparisonGroup(w http.ResponseWriter, request *http.Request) {
	value, ok := r.dependencies.ComparisonGroup(model.GroupID(chi.URLParam(request, "id")))
	if !ok {
		http.Error(w, "comparison group not found", http.StatusNotFound)
		return
	}
	writeJSON(w, value)
}

func (r *Router) updateComparisonGroup(w http.ResponseWriter, request *http.Request) {
	id := model.GroupID(chi.URLParam(request, "id"))
	if _, ok := r.dependencies.ComparisonGroup(id); !ok {
		http.Error(w, "comparison group not found", http.StatusNotFound)
		return
	}
	var input comparisonGroupRequest
	if err := decodeMeasurementBody(request, &input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	value, err := r.dependencies.UpdateComparisonGroup(id, input.Name, input.RunIDs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, value)
}

func (r *Router) comparisonGroupAnalysis(w http.ResponseWriter, request *http.Request) {
	value, err := r.dependencies.AnalyzeComparisonGroup(model.GroupID(chi.URLParam(request, "id")))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, value)
}
