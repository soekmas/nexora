// Package pos provides HTTP endpoints and request dispatching for POS session & cash management.
package pos

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"example.com/nexora/backend/internal/platform/apperr"
	"example.com/nexora/backend/internal/platform/auth"
	"example.com/nexora/backend/internal/platform/idempotency"
	platformmw "example.com/nexora/backend/internal/platform/transport/http/middleware"
)

// Handler exposes POS session capabilities via HTTP endpoints.
type Handler struct {
	service   Service
	idemStore idempotency.Store
	logger    *slog.Logger
}

// NewHandler constructs a new POS HTTP Handler.
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

// Routes returns the canonical domain router mounted at /api/v1/pos.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Route("/sessions", func(sr chi.Router) {
		h.registerSessionRoutes(sr)
	})

	return r
}

// CompatibilityRoutes returns the compatibility alias router mounted at /pos.
func (h *Handler) CompatibilityRoutes() chi.Router {
	r := chi.NewRouter()

	r.Route("/sessions", func(sr chi.Router) {
		h.registerSessionRoutes(sr)
	})

	return r
}

func (h *Handler) registerSessionRoutes(r chi.Router) {
	// Mutating routes with idempotency middleware if available
	wrapIdem := func(op string, handler http.HandlerFunc) http.HandlerFunc {
		if h.idemStore != nil {
			return idempotency.RequireIdempotency(h.idemStore, op, 24*time.Hour)(handler).ServeHTTP
		}
		return handler
	}

	r.Post("/open", wrapIdem("pos.session.open", h.handleOpenSession))
	r.Get("/{id}", h.handleGetSession)
	r.Post("/{id}/cash-in", wrapIdem("pos.session.cash-in", h.handleCashIn))
	r.Post("/{id}/cash-out", wrapIdem("pos.session.cash-out", h.handleCashOut))
	r.Post("/{id}/close", wrapIdem("pos.session.close", h.handleCloseSession))
	r.Post("/{id}/approve-variance", wrapIdem("pos.session.approve-variance", h.handleApproveVariance))
	r.Get("/{id}/summary", h.handleGetSessionSummary)
}

// ============================================================================
// Endpoint Handlers
// ============================================================================

func (h *Handler) handleOpenSession(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermSessionOpen); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	var req OpenSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	session, err := h.service.OpenSession(r.Context(), req)
	if err != nil {
		h.mapAndRespondError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusCreated, session)
}

func (h *Handler) handleGetSession(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermSessionRead); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	idStr := chi.URLParam(r, "id")
	sessionID, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid session identifier"))
		return
	}

	canReview := principal.HasAnyPermission(PermSessionVarianceReview, PermSessionVarianceApprove)
	session, err := h.service.GetSession(r.Context(), sessionID, canReview)
	if err != nil {
		h.mapAndRespondError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusOK, session)
}

func (h *Handler) handleCashIn(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermCashMovementCreate); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	idStr := chi.URLParam(r, "id")
	sessionID, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid session identifier"))
		return
	}

	var req CashMovementRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	movement, err := h.service.CashIn(r.Context(), sessionID, req)
	if err != nil {
		h.mapAndRespondError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusOK, movement)
}

func (h *Handler) handleCashOut(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermCashMovementCreate); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	idStr := chi.URLParam(r, "id")
	sessionID, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid session identifier"))
		return
	}

	var req CashMovementRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	movement, err := h.service.CashOut(r.Context(), sessionID, req)
	if err != nil {
		h.mapAndRespondError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusOK, movement)
}

func (h *Handler) handleCloseSession(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermSessionClose); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	idStr := chi.URLParam(r, "id")
	sessionID, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid session identifier"))
		return
	}

	var req CloseSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	canReview := principal.HasAnyPermission(PermSessionVarianceReview, PermSessionVarianceApprove)
	session, err := h.service.CloseSession(r.Context(), sessionID, req, canReview)
	if err != nil {
		h.mapAndRespondError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusOK, session)
}

func (h *Handler) handleApproveVariance(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermSessionVarianceApprove); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	idStr := chi.URLParam(r, "id")
	sessionID, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid session identifier"))
		return
	}

	var req ApproveVarianceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	session, err := h.service.ApproveVariance(r.Context(), sessionID, req)
	if err != nil {
		h.mapAndRespondError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusOK, session)
}

func (h *Handler) handleGetSessionSummary(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if err := checkPermission(principal, PermSessionRead); err != nil {
		apperr.RespondError(w, reqID, err)
		return
	}

	idStr := chi.URLParam(r, "id")
	sessionID, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid session identifier"))
		return
	}

	canReview := principal.HasAnyPermission(PermSessionVarianceReview, PermSessionVarianceApprove)
	summary, err := h.service.GetSessionSummary(r.Context(), sessionID, canReview)
	if err != nil {
		h.mapAndRespondError(w, reqID, err)
		return
	}

	respondJSON(w, http.StatusOK, summary)
}

// ============================================================================
// Error Mapping
// ============================================================================

func (h *Handler) mapAndRespondError(w http.ResponseWriter, reqID string, err error) {
	switch {
	case errors.Is(err, ErrSessionNotFound), errors.Is(err, ErrTerminalNotFound), errors.Is(err, ErrStoreNotFound):
		apperr.RespondError(w, reqID, apperr.NewNotFoundError(err.Error()))

	case errors.Is(err, ErrTerminalSessionActive):
		apperr.RespondError(w, reqID, apperr.NewConflictError("TERMINAL_SESSION_ACTIVE", "Terminal already has an active session"))

	case errors.Is(err, ErrInvalidSessionStatus):
		apperr.RespondError(w, reqID, apperr.NewConflictError("INVALID_SESSION_STATE", err.Error()))

	case errors.Is(err, ErrSessionVersionConflict):
		apperr.RespondError(w, reqID, apperr.NewConflictError("CONCURRENCY_CONFLICT", "Session modified concurrently"))

	case errors.Is(err, ErrInsufficientDrawerCash):
		// CORR-POS-001: Zero numeric disclosure in over-withdrawal error
		appErr := apperr.NewAppError(http.StatusUnprocessableEntity, "INSUFFICIENT_DRAWER_CASH", "Cash out exceeds available drawer cash")
		apperr.RespondError(w, reqID, appErr)

	case errors.Is(err, ErrSoDViolation):
		appErr := apperr.NewForbiddenError("Cashiers cannot approve variance for their own shift")
		appErr.Detail.Code = "SOD_VIOLATION"
		apperr.RespondError(w, reqID, appErr)

	case errors.Is(err, ErrBlindCountRestricted):
		appErr := apperr.NewForbiddenError("Session summary is restricted to supervisors during active shift")
		appErr.Detail.Code = "BLIND_COUNT_RESTRICTED"
		apperr.RespondError(w, reqID, appErr)

	case errors.Is(err, ErrNegativeOpeningCash):
		apperr.RespondError(w, reqID, apperr.NewValidationError("opening_cash", err.Error()))

	case errors.Is(err, ErrNegativeClosingCash):
		apperr.RespondError(w, reqID, apperr.NewValidationError("closing_cash", err.Error()))

	case errors.Is(err, ErrInvalidAmount):
		apperr.RespondError(w, reqID, apperr.NewValidationError("amount", err.Error()))

	case errors.Is(err, ErrReasonRequired), errors.Is(err, ErrReasonTooShort):
		apperr.RespondError(w, reqID, apperr.NewValidationError("reason", err.Error()))

	case errors.Is(err, ErrApprovalReasonTooShort):
		apperr.RespondError(w, reqID, apperr.NewValidationError("reason", err.Error()))

	case errors.Is(err, ErrTerminalInactive):
		apperr.RespondError(w, reqID, apperr.NewValidationError("terminal_id", err.Error()))

	case errors.Is(err, ErrStoreInactive):
		apperr.RespondError(w, reqID, apperr.NewValidationError("store_id", err.Error()))

	case errors.Is(err, ErrCashierInactive):
		apperr.RespondError(w, reqID, apperr.NewValidationError("cashier_user_id", err.Error()))

	case errors.Is(err, ErrCurrencyRequired), errors.Is(err, ErrInvalidCurrency):
		apperr.RespondError(w, reqID, apperr.NewValidationError("currency", err.Error()))

	default:
		h.logger.Error("Unhandled pos service error", slog.String("error", err.Error()), slog.String("request_id", reqID))
		apperr.RespondError(w, reqID, apperr.NewInternalError("An unexpected error occurred"))
	}
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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
