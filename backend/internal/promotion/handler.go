// Package promotion provides HTTP routing and handlers for promotion management and evaluation.
package promotion

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"example.com/nexora/backend/internal/platform/apperr"
	"example.com/nexora/backend/internal/platform/auth"
	"example.com/nexora/backend/internal/platform/idempotency"
	"example.com/nexora/backend/internal/platform/postgres"
	platformmw "example.com/nexora/backend/internal/platform/transport/http/middleware"
)

// Handler handles HTTP requests for promotion operations.
type Handler struct {
	service   Service
	idemStore idempotency.Store
	logger    *slog.Logger
}

// NewHandler constructs a new promotion HTTP Handler.
func NewHandler(service Service, idemStore idempotency.Store, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		service:   service,
		idemStore: idemStore,
		logger:    logger,
	}
}

// Routes returns the canonical router mounted at /api/v1/promotions.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()

	// Evaluation endpoint
	r.Post("/evaluate", h.handleEvaluatePromotions)

	// Promotion CRUD and management
	r.Get("/", h.handleListPromotions)
	r.Post("/", h.wrapIdem("promotion.create", h.handleCreatePromotion))
	r.Get("/{id}", h.handleGetPromotion)
	r.Put("/{id}", h.handleUpdateDraftPromotion)
	r.Post("/{id}/draft-version", h.wrapIdem("promotion.create-draft-version", h.handleCreateDraftVersion))

	// State machine lifecycle transitions
	r.Post("/{id}/submit", h.wrapIdem("promotion.submit", h.handleSubmitPromotion))
	r.Post("/{id}/approve", h.wrapIdem("promotion.approve", h.handleApprovePromotion))
	r.Post("/{id}/reject", h.wrapIdem("promotion.reject", h.handleRejectPromotion))
	r.Post("/{id}/activate", h.wrapIdem("promotion.activate", h.handleActivatePromotion))
	r.Post("/{id}/pause", h.wrapIdem("promotion.pause", h.handlePausePromotion))
	r.Post("/{id}/deactivate", h.wrapIdem("promotion.deactivate", h.handlePausePromotion))

	return r
}

// CompatibilityRoutes returns the compatibility router mounted at /promotions.
func (h *Handler) CompatibilityRoutes() chi.Router {
	return h.Routes()
}

func (h *Handler) wrapIdem(op string, handler http.HandlerFunc) http.HandlerFunc {
	if h.idemStore != nil {
		return idempotency.Idempotent(h.idemStore, op, false, 24*time.Hour)(handler).ServeHTTP
	}
	return handler
}

func checkPermission(principal *auth.AuthenticatedPrincipal, perm string) *apperr.AppError {
	if principal == nil {
		return apperr.NewUnauthorizedError("Authentication required")
	}
	if principal.HasPermission(perm) {
		return nil
	}
	return auth.EvaluatePermission(principal, perm)
}

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func (h *Handler) handleError(w http.ResponseWriter, reqID string, err error) {
	var stateErr *StateTransitionError
	switch {
	case errors.Is(err, ErrSoDViolation):
		apperr.RespondError(w, reqID, &apperr.AppError{
			HTTPStatus: http.StatusForbidden,
			Detail: apperr.ErrorDetail{
				Code:    "SOD_VIOLATION",
				Message: err.Error(),
			},
		})

	case errors.Is(err, ErrPromotionNotFound):
		apperr.RespondError(w, reqID, apperr.NewNotFoundError(err.Error()))

	case errors.Is(err, ErrPromotionCodeExists):
		apperr.RespondError(w, reqID, apperr.NewConflictError("PROMOTION_CODE_EXISTS", err.Error()))

	case errors.Is(err, ErrPromotionNotEditable):
		apperr.RespondError(w, reqID, apperr.NewConflictError("PROMOTION_NOT_EDITABLE", err.Error()))

	case errors.Is(err, ErrVersionConflict):
		apperr.RespondError(w, reqID, apperr.NewConflictError("VERSION_CONFLICT", "Resource was modified concurrently"))

	case errors.As(err, &stateErr), errors.Is(err, ErrInvalidStateTransition):
		apperr.RespondError(w, reqID, apperr.NewConflictError("INVALID_STATE_TRANSITION", err.Error()))

	case errors.Is(err, ErrReasonRequired):
		apperr.RespondError(w, reqID, apperr.NewValidationError("reason", err.Error()))

	case errors.Is(err, ErrUsageLimitExceeded):
		apperr.RespondError(w, reqID, apperr.NewConflictError("USAGE_LIMIT_EXCEEDED", err.Error()))

	case errors.Is(err, ErrInvalidCondition), errors.Is(err, ErrInvalidReward):
		apperr.RespondError(w, reqID, apperr.NewValidationError("configuration", err.Error()))

	default:
		var appErr *apperr.AppError
		if errors.As(err, &appErr) {
			apperr.RespondError(w, reqID, appErr)
			return
		}

		h.logger.Error("Unhandled promotion service error", slog.String("error", err.Error()), slog.String("request_id", reqID))
		apperr.RespondError(w, reqID, apperr.NewInternalError("An unexpected error occurred"))
	}
}

func ensurePrincipalContext(r *http.Request, principal *auth.AuthenticatedPrincipal) *http.Request {
	ctx := r.Context()
	if _, ok := postgres.FromContext(ctx); !ok && principal != nil {
		ctx = postgres.WithPrincipalContext(ctx, postgres.PrincipalContext{
			TenantID: principal.TenantID,
			UserID:   principal.UserID,
		})
	}
	return r.WithContext(ctx)
}

// ============================================================================
// Endpoint Handlers
// ============================================================================

func (h *Handler) handleEvaluatePromotions(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermPromotionEvaluate); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	r = ensurePrincipalContext(r, principal)

	var req EvaluateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	output, err := h.service.EvaluatePromotions(r.Context(), req)
	if err != nil {
		h.handleError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusOK, output)
}

func (h *Handler) handleListPromotions(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermPromotionRead); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	r = ensurePrincipalContext(r, principal)

	q := r.URL.Query()
	filter := PromotionFilter{
		Page:     1,
		PageSize: 50,
	}
	if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 0 {
		filter.Page = p
	}
	if ps, err := strconv.Atoi(q.Get("page_size")); err == nil && ps > 0 {
		if ps > 200 {
			ps = 200
		}
		filter.PageSize = ps
	}
	if status := q.Get("status"); status != "" {
		filter.Status = &status
	}
	if channel := q.Get("channel"); channel != "" {
		filter.Channel = &channel
	}
	if search := q.Get("search"); search != "" {
		filter.Search = &search
	}

	promos, total, err := h.service.ListPromotions(r.Context(), filter)
	if err != nil {
		h.handleError(w, reqID, err)
		return
	}

	totalPages := 0
	if filter.PageSize > 0 {
		totalPages = (total + filter.PageSize - 1) / filter.PageSize
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"items":     promos,
		"total":     total,
		"page":      filter.Page,
		"page_size": filter.PageSize,
		"meta": map[string]any{
			"page":        filter.Page,
			"page_size":   filter.PageSize,
			"total_items": total,
			"total_pages": totalPages,
		},
	})
}

func (h *Handler) handleCreatePromotion(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermPromotionCreate); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	r = ensurePrincipalContext(r, principal)

	var req CreatePromotionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	promo, err := h.service.CreatePromotion(r.Context(), req)
	if err != nil {
		h.handleError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusCreated, promo)
}

func (h *Handler) handleGetPromotion(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermPromotionRead); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	r = ensurePrincipalContext(r, principal)

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid promotion ID format"))
		return
	}

	promo, err := h.service.GetPromotion(r.Context(), id)
	if err != nil {
		h.handleError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusOK, promo)
}

func (h *Handler) handleUpdateDraftPromotion(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermPromotionUpdate); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	r = ensurePrincipalContext(r, principal)

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid promotion ID format"))
		return
	}

	var req UpdatePromotionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	promo, err := h.service.UpdateDraftPromotion(r.Context(), id, req)
	if err != nil {
		h.handleError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusOK, promo)
}

func (h *Handler) handleCreateDraftVersion(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermPromotionUpdate); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	r = ensurePrincipalContext(r, principal)

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid promotion ID format"))
		return
	}

	var req CreatePromotionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	promo, err := h.service.CreateDraftVersion(r.Context(), id, req)
	if err != nil {
		h.handleError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusCreated, promo)
}

func (h *Handler) handleSubmitPromotion(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermPromotionSubmit); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	r = ensurePrincipalContext(r, principal)

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid promotion ID format"))
		return
	}

	var req TransitionRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	promo, err := h.service.SubmitPromotion(r.Context(), id, req)
	if err != nil {
		h.handleError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusOK, promo)
}

func (h *Handler) handleApprovePromotion(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermPromotionApprove); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	r = ensurePrincipalContext(r, principal)

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid promotion ID format"))
		return
	}

	var req TransitionRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if req.Reason == "" && req.Comment != "" {
		req.Reason = req.Comment
	}

	promo, err := h.service.ApprovePromotion(r.Context(), id, req)
	if err != nil {
		h.handleError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusOK, promo)
}

func (h *Handler) handleRejectPromotion(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermPromotionApprove); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	r = ensurePrincipalContext(r, principal)

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid promotion ID format"))
		return
	}

	var req TransitionRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if req.Reason == "" && req.Comment != "" {
		req.Reason = req.Comment
	}

	promo, err := h.service.RejectPromotion(r.Context(), id, req)
	if err != nil {
		h.handleError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusOK, promo)
}

func (h *Handler) handleActivatePromotion(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermPromotionActivate); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	r = ensurePrincipalContext(r, principal)

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid promotion ID format"))
		return
	}

	var req TransitionRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	promo, err := h.service.ActivatePromotion(r.Context(), id, req)
	if err != nil {
		h.handleError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusOK, promo)
}

func (h *Handler) handlePausePromotion(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermPromotionPause); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	r = ensurePrincipalContext(r, principal)

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid promotion ID format"))
		return
	}

	var req TransitionRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if req.Reason == "" && req.Comment != "" {
		req.Reason = req.Comment
	}

	promo, err := h.service.PausePromotion(r.Context(), id, req)
	if err != nil {
		h.handleError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusOK, promo)
}
