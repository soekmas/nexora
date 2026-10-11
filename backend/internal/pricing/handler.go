// Package pricing provides HTTP routing and handlers for pricing and quote calculations.
package pricing

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"example.com/nexora/backend/internal/platform/apperr"
	"example.com/nexora/backend/internal/platform/auth"
	platformmw "example.com/nexora/backend/internal/platform/transport/http/middleware"
)

// Handler handles HTTP requests for pricing operations.
type Handler struct {
	service Service
	logger  *slog.Logger
}

// NewHandler constructs a new pricing HTTP Handler.
func NewHandler(service Service, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		service: service,
		logger:  logger,
	}
}

// Routes returns the canonical router mounted at /api/v1/pricing.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()

	// Quote endpoint
	r.Post("/quote", h.handleCalculateQuote)

	// Price Lists
	r.Route("/price-lists", func(plr chi.Router) {
		plr.Get("/", h.handleListPriceLists)
		plr.Post("/", h.handleCreatePriceList)
		plr.Get("/{id}", h.handleGetPriceList)
		plr.Put("/{id}", h.handleUpdatePriceList)
	})

	// Price Rules
	r.Route("/rules", func(prr chi.Router) {
		prr.Get("/", h.handleListPriceRules)
		prr.Post("/", h.handleCreatePriceRule)
		prr.Get("/{id}", h.handleGetPriceRule)
		prr.Put("/{id}", h.handleUpdatePriceRule)
		prr.Post("/{id}/submit", h.handleSubmitPriceRule)
		prr.Post("/{id}/approve", h.handleApprovePriceRule)
		prr.Post("/{id}/reject", h.handleRejectPriceRule)
		prr.Post("/{id}/activate", h.handleActivatePriceRule)
		prr.Post("/{id}/deactivate", h.handleDeactivatePriceRule)
	})

	return r
}

// CompatibilityRoutes returns the compatibility router mounted at /pricing.
func (h *Handler) CompatibilityRoutes() chi.Router {
	return h.Routes()
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
	var missingPriceErr *MissingPriceError
	switch {
	case errors.As(err, &missingPriceErr):
		apperr.RespondError(w, reqID, &apperr.AppError{
			HTTPStatus: http.StatusUnprocessableEntity,
			Detail: apperr.ErrorDetail{
				Code:    "PRICE_NOT_FOUND",
				Message: "One or more cart items have no valid active price rule",
				Field:   "items",
				Details: map[string]any{
					"unpriced_items": missingPriceErr.UnpricedItems,
				},
			},
		})

	case errors.Is(err, ErrPriceListNotFound):
		apperr.RespondError(w, reqID, &apperr.AppError{
			HTTPStatus: http.StatusUnprocessableEntity,
			Detail: apperr.ErrorDetail{
				Code:    "PRICE_LIST_NOT_FOUND",
				Message: err.Error(),
			},
		})

	case errors.Is(err, ErrCurrencyMismatch):
		apperr.RespondError(w, reqID, &apperr.AppError{
			HTTPStatus: http.StatusBadRequest,
			Detail: apperr.ErrorDetail{
				Code:    "CURRENCY_MISMATCH",
				Message: err.Error(),
			},
		})

	case errors.Is(err, ErrVersionConflict):
		apperr.RespondError(w, reqID, apperr.NewConflictError("VERSION_CONFLICT", "Resource was modified concurrently"))

	case errors.Is(err, ErrDuplicateDefaultList):
		apperr.RespondError(w, reqID, apperr.NewConflictError("DUPLICATE_DEFAULT_PRICE_LIST", err.Error()))

	case errors.Is(err, ErrDefaultCompanyNotNull):
		apperr.RespondError(w, reqID, apperr.NewValidationError("company_id", err.Error()))

	case errors.Is(err, ErrPriceListNotFound404), errors.Is(err, ErrPriceRuleNotFound404), errors.Is(err, ErrStoreNotFound):
		apperr.RespondError(w, reqID, apperr.NewNotFoundError(err.Error()))

	case errors.Is(err, ErrInvalidStateTransition):
		apperr.RespondError(w, reqID, apperr.NewConflictError("INVALID_STATE_TRANSITION", err.Error()))

	case errors.Is(err, ErrRuleNotEditable):
		apperr.RespondError(w, reqID, apperr.NewConflictError("RULE_NOT_EDITABLE", err.Error()))

	case errors.Is(err, ErrUnauthorizedPriceList):
		apperr.RespondError(w, reqID, apperr.NewForbiddenError(err.Error()))

	case errors.Is(err, ErrReasonRequired):
		apperr.RespondError(w, reqID, apperr.NewValidationError("reason", err.Error()))

	case errors.Is(err, ErrVariantProductMismatch):
		apperr.RespondError(w, reqID, apperr.NewValidationError("variant_id", err.Error()))

	case errors.Is(err, ErrStoreInactive):
		apperr.RespondError(w, reqID, apperr.NewValidationError("store_id", err.Error()))

	case errors.Is(err, ErrPriceListArchived):
		apperr.RespondError(w, reqID, apperr.NewConflictError("PRICE_LIST_ARCHIVED", err.Error()))

	default:
		h.logger.Error("Unhandled pricing service error", slog.String("error", err.Error()), slog.String("request_id", reqID))
		apperr.RespondError(w, reqID, apperr.NewInternalError("An unexpected error occurred"))
	}
}

// ============================================================================
// Endpoint Handlers
// ============================================================================

func (h *Handler) handleCalculateQuote(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermPriceRead); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	var req PriceQuoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	quote, err := h.service.CalculateQuote(r.Context(), req)
	if err != nil {
		h.handleError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusOK, quote)
}

func (h *Handler) handleListPriceLists(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermPriceRead); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	q := r.URL.Query()
	filter := PriceListFilter{}
	if cur := q.Get("currency"); cur != "" {
		filter.Currency = &cur
	}
	if ch := q.Get("channel"); ch != "" {
		filter.Channel = &ch
	}
	if st := q.Get("status"); st != "" {
		filter.Status = &st
	}
	if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 0 {
		filter.Page = p
	}
	if ps, err := strconv.Atoi(q.Get("page_size")); err == nil && ps > 0 {
		filter.PageSize = ps
	}

	lists, total, err := h.service.ListPriceLists(r.Context(), filter)
	if err != nil {
		h.handleError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"items":     lists,
		"total":     total,
		"page":      filter.Page,
		"page_size": filter.PageSize,
	})
}

func (h *Handler) handleCreatePriceList(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermPriceListCreate); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	var req CreatePriceListRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	pl, err := h.service.CreatePriceList(r.Context(), req)
	if err != nil {
		h.handleError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusCreated, pl)
}

func (h *Handler) handleGetPriceList(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermPriceRead); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	pl, err := h.service.GetPriceList(r.Context(), id)
	if err != nil {
		h.handleError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusOK, pl)
}

func (h *Handler) handleUpdatePriceList(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermPriceListUpdate); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	var req UpdatePriceListRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	pl, err := h.service.UpdatePriceList(r.Context(), id, req)
	if err != nil {
		h.handleError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusOK, pl)
}

func (h *Handler) handleListPriceRules(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermPriceRuleRead); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	q := r.URL.Query()
	filter := PriceRuleFilter{}
	if plIDStr := q.Get("price_list_id"); plIDStr != "" {
		if id, err := uuid.Parse(plIDStr); err == nil {
			filter.PriceListID = &id
		}
	}
	if pIDStr := q.Get("product_id"); pIDStr != "" {
		if id, err := uuid.Parse(pIDStr); err == nil {
			filter.ProductID = &id
		}
	}
	if vIDStr := q.Get("variant_id"); vIDStr != "" {
		if id, err := uuid.Parse(vIDStr); err == nil {
			filter.VariantID = &id
		}
	}
	if sIDStr := q.Get("store_id"); sIDStr != "" {
		if id, err := uuid.Parse(sIDStr); err == nil {
			filter.StoreID = &id
		}
	}
	if cgIDStr := q.Get("customer_group_id"); cgIDStr != "" {
		if id, err := uuid.Parse(cgIDStr); err == nil {
			filter.CustomerGroupID = &id
		}
	}
	if st := q.Get("status"); st != "" {
		filter.Status = &st
	}
	if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 0 {
		filter.Page = p
	}
	if ps, err := strconv.Atoi(q.Get("page_size")); err == nil && ps > 0 {
		filter.PageSize = ps
	}

	rules, total, err := h.service.ListPriceRules(r.Context(), filter)
	if err != nil {
		h.handleError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"items":     rules,
		"total":     total,
		"page":      filter.Page,
		"page_size": filter.PageSize,
	})
}

func (h *Handler) handleCreatePriceRule(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermPriceRuleCreate); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	var req CreatePriceRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	pr, err := h.service.CreatePriceRule(r.Context(), req)
	if err != nil {
		h.handleError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusCreated, pr)
}

func (h *Handler) handleGetPriceRule(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermPriceRuleRead); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	pr, err := h.service.GetPriceRule(r.Context(), id)
	if err != nil {
		h.handleError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusOK, pr)
}

func (h *Handler) handleUpdatePriceRule(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermPriceRuleUpdate); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	var req UpdatePriceRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	pr, err := h.service.UpdatePriceRule(r.Context(), id, req)
	if err != nil {
		h.handleError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusOK, pr)
}

func (h *Handler) handleSubmitPriceRule(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermPriceRuleSubmit); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	var req RuleTransitionRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	pr, err := h.service.SubmitPriceRule(r.Context(), id, req)
	if err != nil {
		h.handleError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusOK, pr)
}

func (h *Handler) handleApprovePriceRule(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermPriceRuleApprove); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	var req RuleTransitionRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	pr, err := h.service.ApprovePriceRule(r.Context(), id, req)
	if err != nil {
		h.handleError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusOK, pr)
}

func (h *Handler) handleRejectPriceRule(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermPriceRuleApprove); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	var req RuleTransitionRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	pr, err := h.service.RejectPriceRule(r.Context(), id, req)
	if err != nil {
		h.handleError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusOK, pr)
}

func (h *Handler) handleActivatePriceRule(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermPriceRuleActivate); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	var req RuleTransitionRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	pr, err := h.service.ActivatePriceRule(r.Context(), id, req)
	if err != nil {
		h.handleError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusOK, pr)
}

func (h *Handler) handleDeactivatePriceRule(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermPriceRuleDeactivate); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	var req RuleTransitionRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	pr, err := h.service.DeactivatePriceRule(r.Context(), id, req)
	if err != nil {
		h.handleError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusOK, pr)
}
