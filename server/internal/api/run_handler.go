package api

import (
	"log"
	"net/http"
	"strings"

	"breckr-server/internal/store"
	"breckr-server/internal/types"
	"breckr-server/internal/utils"
)

type RunHandler struct {
	logger   *log.Logger
	runStore store.RunStore
}

func NewRunHandler(logger *log.Logger, runStore store.RunStore) *RunHandler {
	return &RunHandler{logger: logger, runStore: runStore}
}

// clampLimit caps the page size so one request cannot pull the entire history.
func clampLimit(raw int) int {
	if raw <= 0 {
		return types.DefaultRunLimit
	}
	if raw > types.MaxRunLimit {
		return types.MaxRunLimit
	}
	return raw
}

func clampOffset(raw int) int {
	if raw < 0 {
		return 0
	}
	return raw
}

func (rh *RunHandler) HandleGetAllRuns(w http.ResponseWriter, r *http.Request) {
	status := utils.ReadStringQueryParam(r, "status", "")
	if status != "" && !types.IsRunStatus(status) {
		labels := make([]string, len(types.RunStatuses))
		for i, candidate := range types.RunStatuses {
			labels[i] = string(candidate)
		}
		utils.WriteError(w, http.StatusBadRequest,
			"status must be one of "+strings.Join(labels, ", ")+".", "")
		return
	}

	// A malformed limit or offset falls back to the default rather than
	// rejecting: the dashboard always sends valid ones, and a hand-written
	// query is better served a first page than an error.
	limit, err := utils.ReadIntQueryParam(r, "limit", types.DefaultRunLimit)
	if err != nil {
		limit = types.DefaultRunLimit
	}
	offset, err := utils.ReadIntQueryParam(r, "offset", 0)
	if err != nil {
		offset = 0
	}

	response, err := rh.runStore.ListRuns(store.ListRunsOptions{
		TaskID: utils.ReadStringQueryParam(r, "task_id", ""),
		Status: types.RunStatus(status),
		Limit:  clampLimit(limit),
		Offset: clampOffset(offset),
	})
	if err != nil {
		rh.logger.Printf("ERROR: ListRuns: %v", err)
		utils.WriteError(w, http.StatusInternalServerError, "internal server error", "")
		return
	}

	utils.WriteJSONResponse(w, http.StatusOK, utils.Envelope{"data": response})
}

func (rh *RunHandler) HandleGetRun(w http.ResponseWriter, r *http.Request) {
	id, err := utils.ReadInt64Param(r, "id")
	if err != nil {
		utils.WriteError(w, http.StatusNotFound, "Run not found.", "")
		return
	}

	run, err := rh.runStore.GetRun(id)
	if err != nil {
		rh.logger.Printf("ERROR: GetRun: %v", err)
		utils.WriteError(w, http.StatusInternalServerError, "internal server error", "")
		return
	}
	if run == nil {
		utils.WriteError(w, http.StatusNotFound, "Run not found.", "")
		return
	}

	utils.WriteJSONResponse(w, http.StatusOK, utils.Envelope{"data": run})
}
