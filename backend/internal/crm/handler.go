// Package crm provides HTTP endpoints and request dispatching for CRM.
package crm

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
	"example.com/nexora/backend/internal/platform/postgres"
	platformmw "example.com/nexora/backend/internal/platform/transport/http/middleware"
)

// Handler exposes CRM capabilities via HTTP endpoints.
type Handler struct {
	service Service
	logger  *slog.Logger
}

// NewHandler constructs a new CRM HTTP Handler.
func NewHandler(service Service, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		service: service,
		logger:  logger,
	}
}

// Routes returns the canonical domain router mounted at /api/v1/crm.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Group(func(protected chi.Router) {
		// Customer routes
		protected.Post("/customers", h.handleCreateCustomer)
		protected.Get("/customers", h.handleListCustomers)
		protected.Get("/customers/{id}", h.handleGetCustomer)
		protected.Patch("/customers/{id}", h.handleUpdateCustomer)
		protected.Post("/customers/{id}/disable", h.handleDisableCustomer)
		protected.Post("/customers/{id}/enable", h.handleEnableCustomer)

		// Address routes
		protected.Get("/customers/{id}/addresses", h.handleListAddresses)
		protected.Post("/customers/{id}/addresses", h.handleCreateAddress)
		protected.Patch("/customers/{id}/addresses/{address_id}", h.handleUpdateAddress)
		protected.Delete("/customers/{id}/addresses/{address_id}", h.handleDeleteAddress)

		// Customer group routes
		protected.Post("/customer-groups", h.handleCreateCustomerGroup)
		protected.Get("/customer-groups", h.handleListCustomerGroups)
		protected.Get("/customer-groups/{id}", h.handleGetCustomerGroup)
		protected.Patch("/customer-groups/{id}", h.handleUpdateCustomerGroup)
	})

	return r
}

// CustomerAliasRoutes returns the compatibility alias router mounted at /api/v1/customers (DEC-B1-018).
func (h *Handler) CustomerAliasRoutes() chi.Router {
	r := chi.NewRouter()

	r.Group(func(protected chi.Router) {
		protected.Post("/", h.handleCreateCustomer)
		protected.Get("/", h.handleListCustomers)
		protected.Get("/{id}", h.handleGetCustomer)
		protected.Patch("/{id}", h.handleUpdateCustomer)
		protected.Post("/{id}/disable", h.handleDisableCustomer)
		protected.Post("/{id}/enable", h.handleEnableCustomer)

		protected.Get("/{id}/addresses", h.handleListAddresses)
		protected.Post("/{id}/addresses", h.handleCreateAddress)
		protected.Patch("/{id}/addresses/{address_id}", h.handleUpdateAddress)
		protected.Delete("/{id}/addresses/{address_id}", h.handleDeleteAddress)
	})

	return r
}

// ============================================================================
// Customer Group Handlers
// ============================================================================

func (h *Handler) handleCreateCustomerGroup(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, PermCustomerGroupCreate); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	var req CreateCustomerGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request body"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	group, err := h.service.CreateCustomerGroup(ctx, &principal.UserID, req)
	if err != nil {
		apperr.RespondError(w, reqID, h.mapDomainError(err))
		return
	}

	h.respondJSON(w, http.StatusOK, group)
}

func (h *Handler) handleGetCustomerGroup(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, PermCustomerGroupRead); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid customer group ID format"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	group, err := h.service.GetCustomerGroup(ctx, id)
	if err != nil {
		apperr.RespondError(w, reqID, h.mapDomainError(err))
		return
	}

	h.respondJSON(w, http.StatusOK, group)
}

func (h *Handler) handleUpdateCustomerGroup(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, PermCustomerGroupUpdate); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid customer group ID format"))
		return
	}

	var req UpdateCustomerGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request body"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	group, err := h.service.UpdateCustomerGroup(ctx, &principal.UserID, id, req)
	if err != nil {
		apperr.RespondError(w, reqID, h.mapDomainError(err))
		return
	}

	h.respondJSON(w, http.StatusOK, group)
}

func (h *Handler) handleListCustomerGroups(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, PermCustomerGroupRead); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	status := r.URL.Query().Get("status")
	page, pageSize, offset := parsePagination(r)

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	groups, total, err := h.service.ListCustomerGroups(ctx, status, pageSize, offset)
	if err != nil {
		apperr.RespondError(w, reqID, h.mapDomainError(err))
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]any{
		"data":       groups,
		"pagination": makePaginationMeta(page, pageSize, total),
	})
}

// ============================================================================
// Customer Handlers
// ============================================================================

func (h *Handler) handleCreateCustomer(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, PermCustomerCreate); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	var req CreateCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request body"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	cust, err := h.service.CreateCustomer(ctx, &principal.UserID, req)
	if err != nil {
		apperr.RespondError(w, reqID, h.mapDomainError(err))
		return
	}

	h.respondJSON(w, http.StatusOK, cust.ToResponse())
}

func (h *Handler) handleGetCustomer(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, PermCustomerRead); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid customer ID format"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	cust, err := h.service.GetCustomer(ctx, id)
	if err != nil {
		apperr.RespondError(w, reqID, h.mapDomainError(err))
		return
	}

	h.respondJSON(w, http.StatusOK, cust.ToResponse())
}

func (h *Handler) handleUpdateCustomer(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, PermCustomerUpdate); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid customer ID format"))
		return
	}

	var req UpdateCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request body"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	cust, err := h.service.UpdateCustomer(ctx, &principal.UserID, id, req)
	if err != nil {
		apperr.RespondError(w, reqID, h.mapDomainError(err))
		return
	}

	h.respondJSON(w, http.StatusOK, cust.ToResponse())
}

func (h *Handler) handleDisableCustomer(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, PermCustomerUpdate); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid customer ID format"))
		return
	}

	var body struct {
		ExpectedVersion *int64 `json:"expected_version,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	cust, err := h.service.DisableCustomer(ctx, &principal.UserID, id, body.ExpectedVersion)
	if err != nil {
		apperr.RespondError(w, reqID, h.mapDomainError(err))
		return
	}

	h.respondJSON(w, http.StatusOK, cust.ToResponse())
}

func (h *Handler) handleEnableCustomer(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, PermCustomerUpdate); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid customer ID format"))
		return
	}

	var body struct {
		ExpectedVersion *int64 `json:"expected_version,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	cust, err := h.service.EnableCustomer(ctx, &principal.UserID, id, body.ExpectedVersion)
	if err != nil {
		apperr.RespondError(w, reqID, h.mapDomainError(err))
		return
	}

	h.respondJSON(w, http.StatusOK, cust.ToResponse())
}

func (h *Handler) handleListCustomers(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, PermCustomerRead); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	page, pageSize, _ := parsePagination(r)
	q := r.URL.Query()

	var groupID *uuid.UUID
	if gStr := q.Get("group_id"); gStr != "" {
		if gid, err := uuid.Parse(gStr); err == nil {
			groupID = &gid
		}
	}

	filter := ListCustomersFilter{
		Query:    q.Get("query"),
		Email:    q.Get("email"),
		Phone:    q.Get("phone"),
		Status:   q.Get("status"),
		GroupID:  groupID,
		Page:     page,
		PageSize: pageSize,
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	customers, total, err := h.service.ListCustomers(ctx, filter)
	if err != nil {
		apperr.RespondError(w, reqID, h.mapDomainError(err))
		return
	}

	responses := make([]CustomerResponse, len(customers))
	for i, c := range customers {
		responses[i] = c.ToResponse()
	}

	h.respondJSON(w, http.StatusOK, map[string]any{
		"data":       responses,
		"pagination": makePaginationMeta(page, pageSize, total),
	})
}

// ============================================================================
// Address Handlers
// ============================================================================

func (h *Handler) handleCreateAddress(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, PermCustomerAddressManage); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	customerID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid customer ID format"))
		return
	}

	var req CreateAddressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request body"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	addr, err := h.service.CreateAddress(ctx, &principal.UserID, customerID, req)
	if err != nil {
		apperr.RespondError(w, reqID, h.mapDomainError(err))
		return
	}

	h.respondJSON(w, http.StatusOK, addr.ToResponse())
}

func (h *Handler) handleListAddresses(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, PermCustomerRead); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	customerID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid customer ID format"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	addresses, err := h.service.ListAddresses(ctx, customerID)
	if err != nil {
		apperr.RespondError(w, reqID, h.mapDomainError(err))
		return
	}

	responses := make([]AddressResponse, len(addresses))
	for i, a := range addresses {
		responses[i] = a.ToResponse()
	}

	h.respondJSON(w, http.StatusOK, responses)
}

func (h *Handler) handleUpdateAddress(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, PermCustomerAddressManage); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	customerID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid customer ID format"))
		return
	}

	addressID, err := uuid.Parse(chi.URLParam(r, "address_id"))
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid address ID format"))
		return
	}

	var req UpdateAddressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request body"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	addr, err := h.service.UpdateAddress(ctx, &principal.UserID, customerID, addressID, req)
	if err != nil {
		apperr.RespondError(w, reqID, h.mapDomainError(err))
		return
	}

	h.respondJSON(w, http.StatusOK, addr.ToResponse())
}

func (h *Handler) handleDeleteAddress(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, PermCustomerAddressManage); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	customerID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid customer ID format"))
		return
	}

	addressID, err := uuid.Parse(chi.URLParam(r, "address_id"))
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid address ID format"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	if err := h.service.DeleteAddress(ctx, &principal.UserID, customerID, addressID); err != nil {
		apperr.RespondError(w, reqID, h.mapDomainError(err))
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]any{"status": "DELETED"})
}

// ============================================================================
// Helpers
// ============================================================================

func (h *Handler) respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func (h *Handler) mapDomainError(err error) *apperr.AppError {
	switch {
	case errors.Is(err, ErrCustomerNotFound):
		return apperr.NewNotFoundError("Customer")
	case errors.Is(err, ErrCustomerGroupNotFound):
		return apperr.NewNotFoundError("CustomerGroup")
	case errors.Is(err, ErrAddressNotFound):
		return apperr.NewNotFoundError("CustomerAddress")
	case errors.Is(err, ErrCustomerNumberConflict):
		return apperr.NewConflictError("CUSTOMER_NUMBER_EXISTS", err.Error())
	case errors.Is(err, ErrCustomerGroupCodeConflict):
		return apperr.NewConflictError("CUSTOMER_GROUP_EXISTS", err.Error())
	case errors.Is(err, ErrCustomerVersionConflict):
		return apperr.NewConflictError("CUSTOMER_VERSION_CONFLICT", err.Error())
	case errors.Is(err, ErrCustomerInactive):
		return apperr.NewConflictError("CUSTOMER_INACTIVE", err.Error())
	case errors.Is(err, ErrCustomerGroupInactive):
		return apperr.NewConflictError("CUSTOMER_GROUP_INACTIVE", err.Error())
	case errors.Is(err, ErrCustomerNameRequired):
		return apperr.NewValidationError("name", err.Error())
	case errors.Is(err, ErrCustomerGroupNameRequired):
		return apperr.NewValidationError("name", err.Error())
	case errors.Is(err, ErrCustomerGroupCodeRequired):
		return apperr.NewValidationError("code", err.Error())
	case errors.Is(err, ErrAddressLineRequired):
		return apperr.NewValidationError("address_line", err.Error())
	case errors.Is(err, ErrInvalidCustomerNumber):
		return apperr.NewValidationError("customer_no", err.Error())
	case errors.Is(err, ErrInvalidEmail):
		return apperr.NewValidationError("email", err.Error())
	case errors.Is(err, ErrInvalidPhone):
		return apperr.NewValidationError("phone", err.Error())
	case errors.Is(err, ErrInvalidAddressType):
		return apperr.NewValidationError("address_type", err.Error())
	case errors.Is(err, ErrInvalidInput):
		return apperr.NewBadRequestError(err.Error())
	case errors.Is(err, postgres.ErrMissingTenantContext):
		return apperr.NewUnauthorizedError("Tenant context missing")
	default:
		h.logger.Error("Unhandled CRM domain error", slog.String("error", err.Error()))
		return apperr.NewInternalError("An unexpected error occurred")
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

func parsePagination(r *http.Request) (page, pageSize, offset int) {
	page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ = strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize <= 0 {
		pageSize, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	offset = (page - 1) * pageSize
	if offStr := r.URL.Query().Get("offset"); offStr != "" {
		if o, err := strconv.Atoi(offStr); err == nil && o >= 0 {
			offset = o
		}
	}
	return page, pageSize, offset
}

// PaginationMeta represents pagination metadata for list responses.
type PaginationMeta struct {
	Page       int   `json:"page"`
	PageSize   int   `json:"page_size"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

func makePaginationMeta(page, pageSize int, total int64) PaginationMeta {
	totalPages := 0
	if pageSize > 0 && total > 0 {
		totalPages = int((total + int64(pageSize) - 1) / int64(pageSize))
	}
	return PaginationMeta{
		Page:       page,
		PageSize:   pageSize,
		Total:      total,
		TotalPages: totalPages,
	}
}
