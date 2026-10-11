package organization

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

// Handler exposes Organization capabilities via HTTP endpoints.
type Handler struct {
	service Service
	logger  *slog.Logger
}

// NewHandler constructs a new Organization HTTP Handler.
func NewHandler(service Service, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		service: service,
		logger:  logger,
	}
}

// Routes sets up Chi routing for Organization endpoints.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Group(func(protected chi.Router) {
		// Company routes
		protected.Post("/companies", h.handleCreateCompany)
		protected.Get("/companies", h.handleListCompanies)
		protected.Get("/companies/{id}", h.handleGetCompany)
		protected.Patch("/companies/{id}", h.handleUpdateCompany)

		// Region routes
		protected.Post("/regions", h.handleCreateRegion)
		protected.Get("/regions", h.handleListRegions)
		protected.Get("/regions/{id}", h.handleGetRegion)
		protected.Patch("/regions/{id}", h.handleUpdateRegion)

		// Store routes
		protected.Post("/stores", h.handleCreateStore)
		protected.Get("/stores", h.handleListStores)
		protected.Get("/stores/{id}", h.handleGetStore)
		protected.Patch("/stores/{id}", h.handleUpdateStore)

		// Warehouse routes
		protected.Post("/warehouses", h.handleCreateWarehouse)
		protected.Get("/warehouses", h.handleListWarehouses)
		protected.Get("/warehouses/{id}", h.handleGetWarehouse)
		protected.Patch("/warehouses/{id}", h.handleUpdateWarehouse)

		// Terminal routes
		protected.Post("/terminals", h.handleRegisterTerminal)
		protected.Get("/terminals", h.handleListTerminals)
		protected.Get("/terminals/{id}", h.handleGetTerminal)
		protected.Patch("/terminals/{id}", h.handleUpdateTerminal)
		protected.Post("/terminals/{id}/rotate-device", h.handleRotateDeviceToken)
		protected.Post("/terminals/{id}/revoke-device", h.handleRevokeDeviceToken)
		protected.Post("/terminals/{id}/verify-device", h.handleVerifyDeviceToken)
	})

	return r
}

func (h *Handler) respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func checkPermission(principal *auth.AuthenticatedPrincipal, perms ...string) *apperr.AppError {
	if principal == nil {
		return apperr.NewUnauthorizedError("Authentication required")
	}
	for _, p := range perms {
		if principal.HasPermission(p) {
			return nil
		}
	}
	return auth.EvaluatePermission(principal, perms[0])
}

// ============================================================================
// Company Handlers
// ============================================================================

func (h *Handler) handleCreateCompany(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "company.create", "organization:company:create"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	var req CreateCompanyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	company, err := h.service.CreateCompany(ctx, &principal.UserID, req)
	if err != nil {
		var appErr *apperr.AppError
		if errors.As(MapToAppError(err), &appErr) {
			apperr.RespondError(w, reqID, appErr)
		} else {
			apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		}
		return
	}

	h.respondJSON(w, http.StatusCreated, company)
}

func (h *Handler) handleGetCompany(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "company.read", "organization:company:read"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	company, err := h.service.GetCompany(ctx, id)
	if err != nil {
		var appErr *apperr.AppError
		if errors.As(MapToAppError(err), &appErr) {
			apperr.RespondError(w, reqID, appErr)
		} else {
			apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		}
		return
	}

	h.respondJSON(w, http.StatusOK, company)
}

func (h *Handler) handleUpdateCompany(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "company.update", "organization:company:update"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	var req UpdateCompanyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	company, err := h.service.UpdateCompany(ctx, &principal.UserID, id, req)
	if err != nil {
		var appErr *apperr.AppError
		if errors.As(MapToAppError(err), &appErr) {
			apperr.RespondError(w, reqID, appErr)
		} else {
			apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		}
		return
	}

	h.respondJSON(w, http.StatusOK, company)
}

func (h *Handler) handleListCompanies(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "company.read", "organization:company:read"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	items, total, err := h.service.ListCompanies(ctx, limit, offset)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// ============================================================================
// Region Handlers
// ============================================================================

func (h *Handler) handleCreateRegion(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "region.create", "organization:region:create"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	var req CreateRegionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	region, err := h.service.CreateRegion(ctx, &principal.UserID, req)
	if err != nil {
		var appErr *apperr.AppError
		if errors.As(MapToAppError(err), &appErr) {
			apperr.RespondError(w, reqID, appErr)
		} else {
			apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		}
		return
	}

	h.respondJSON(w, http.StatusCreated, region)
}

func (h *Handler) handleGetRegion(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "region.read", "organization:region:read"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	region, err := h.service.GetRegion(ctx, id)
	if err != nil {
		var appErr *apperr.AppError
		if errors.As(MapToAppError(err), &appErr) {
			apperr.RespondError(w, reqID, appErr)
		} else {
			apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		}
		return
	}

	h.respondJSON(w, http.StatusOK, region)
}

func (h *Handler) handleUpdateRegion(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "region.update", "organization:region:update"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	var req UpdateRegionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	region, err := h.service.UpdateRegion(ctx, &principal.UserID, id, req)
	if err != nil {
		var appErr *apperr.AppError
		if errors.As(MapToAppError(err), &appErr) {
			apperr.RespondError(w, reqID, appErr)
		} else {
			apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		}
		return
	}

	h.respondJSON(w, http.StatusOK, region)
}

func (h *Handler) handleListRegions(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "region.read", "organization:region:read"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	var companyID *uuid.UUID
	if cidStr := r.URL.Query().Get("company_id"); cidStr != "" {
		if parsed, err := uuid.Parse(cidStr); err == nil {
			companyID = &parsed
		}
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	items, total, err := h.service.ListRegions(ctx, companyID, limit, offset)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// ============================================================================
// Store Handlers
// ============================================================================

func (h *Handler) handleCreateStore(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "store.create", "organization:store:create"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	var req CreateStoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	store, err := h.service.CreateStore(ctx, &principal.UserID, req)
	if err != nil {
		var appErr *apperr.AppError
		if errors.As(MapToAppError(err), &appErr) {
			apperr.RespondError(w, reqID, appErr)
		} else {
			apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		}
		return
	}

	h.respondJSON(w, http.StatusCreated, store)
}

func (h *Handler) handleGetStore(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "store.read", "organization:store:read"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	store, err := h.service.GetStore(ctx, id)
	if err != nil {
		var appErr *apperr.AppError
		if errors.As(MapToAppError(err), &appErr) {
			apperr.RespondError(w, reqID, appErr)
		} else {
			apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		}
		return
	}

	h.respondJSON(w, http.StatusOK, store)
}

func (h *Handler) handleUpdateStore(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "store.update", "organization:store:update", "store.disable"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	var req UpdateStoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	store, err := h.service.UpdateStore(ctx, &principal.UserID, id, req)
	if err != nil {
		var appErr *apperr.AppError
		if errors.As(MapToAppError(err), &appErr) {
			apperr.RespondError(w, reqID, appErr)
		} else {
			apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		}
		return
	}

	h.respondJSON(w, http.StatusOK, store)
}

func (h *Handler) handleListStores(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "store.read", "organization:store:read"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	var companyID *uuid.UUID
	if cidStr := r.URL.Query().Get("company_id"); cidStr != "" {
		if parsed, err := uuid.Parse(cidStr); err == nil {
			companyID = &parsed
		}
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	items, total, err := h.service.ListStores(ctx, companyID, limit, offset)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// ============================================================================
// Warehouse Handlers
// ============================================================================

func (h *Handler) handleCreateWarehouse(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "warehouse.create", "organization:warehouse:create"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	var req CreateWarehouseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	warehouse, err := h.service.CreateWarehouse(ctx, &principal.UserID, req)
	if err != nil {
		var appErr *apperr.AppError
		if errors.As(MapToAppError(err), &appErr) {
			apperr.RespondError(w, reqID, appErr)
		} else {
			apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		}
		return
	}

	h.respondJSON(w, http.StatusCreated, warehouse)
}

func (h *Handler) handleGetWarehouse(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "warehouse.read", "organization:warehouse:read"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	warehouse, err := h.service.GetWarehouse(ctx, id)
	if err != nil {
		var appErr *apperr.AppError
		if errors.As(MapToAppError(err), &appErr) {
			apperr.RespondError(w, reqID, appErr)
		} else {
			apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		}
		return
	}

	h.respondJSON(w, http.StatusOK, warehouse)
}

func (h *Handler) handleUpdateWarehouse(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "warehouse.update", "organization:warehouse:update"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	var req UpdateWarehouseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	warehouse, err := h.service.UpdateWarehouse(ctx, &principal.UserID, id, req)
	if err != nil {
		var appErr *apperr.AppError
		if errors.As(MapToAppError(err), &appErr) {
			apperr.RespondError(w, reqID, appErr)
		} else {
			apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		}
		return
	}

	h.respondJSON(w, http.StatusOK, warehouse)
}

func (h *Handler) handleListWarehouses(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "warehouse.read", "organization:warehouse:read"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	var companyID, storeID *uuid.UUID
	if cidStr := r.URL.Query().Get("company_id"); cidStr != "" {
		if parsed, err := uuid.Parse(cidStr); err == nil {
			companyID = &parsed
		}
	}
	if sidStr := r.URL.Query().Get("store_id"); sidStr != "" {
		if parsed, err := uuid.Parse(sidStr); err == nil {
			storeID = &parsed
		}
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	items, total, err := h.service.ListWarehouses(ctx, companyID, storeID, limit, offset)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// ============================================================================
// Terminal & Device Token Handlers
// ============================================================================

func (h *Handler) handleRegisterTerminal(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "terminal.register", "organization:terminal:register"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	var req RegisterTerminalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	res, err := h.service.RegisterTerminal(ctx, &principal.UserID, req)
	if err != nil {
		var appErr *apperr.AppError
		if errors.As(MapToAppError(err), &appErr) {
			apperr.RespondError(w, reqID, appErr)
		} else {
			apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		}
		return
	}

	h.respondJSON(w, http.StatusCreated, res)
}

func (h *Handler) handleGetTerminal(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "terminal.read", "organization:terminal:read"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	terminal, err := h.service.GetTerminal(ctx, id)
	if err != nil {
		var appErr *apperr.AppError
		if errors.As(MapToAppError(err), &appErr) {
			apperr.RespondError(w, reqID, appErr)
		} else {
			apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		}
		return
	}

	h.respondJSON(w, http.StatusOK, terminal)
}

func (h *Handler) handleUpdateTerminal(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "terminal.update", "organization:terminal:update", "terminal.disable", "terminal.retire"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	var req UpdateTerminalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	terminal, err := h.service.UpdateTerminal(ctx, &principal.UserID, id, req)
	if err != nil {
		var appErr *apperr.AppError
		if errors.As(MapToAppError(err), &appErr) {
			apperr.RespondError(w, reqID, appErr)
		} else {
			apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		}
		return
	}

	h.respondJSON(w, http.StatusOK, terminal)
}

func (h *Handler) handleListTerminals(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "terminal.read", "organization:terminal:read"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	var storeID *uuid.UUID
	if sidStr := r.URL.Query().Get("store_id"); sidStr != "" {
		if parsed, err := uuid.Parse(sidStr); err == nil {
			storeID = &parsed
		}
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	items, total, err := h.service.ListTerminals(ctx, storeID, limit, offset)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (h *Handler) handleRotateDeviceToken(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "terminal.register", "organization:terminal:register"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	res, err := h.service.RotateDeviceToken(ctx, &principal.UserID, id)
	if err != nil {
		var appErr *apperr.AppError
		if errors.As(MapToAppError(err), &appErr) {
			apperr.RespondError(w, reqID, appErr)
		} else {
			apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		}
		return
	}

	h.respondJSON(w, http.StatusOK, res)
}

func (h *Handler) handleRevokeDeviceToken(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "terminal.disable", "organization:terminal:disable"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	if err := h.service.RevokeDeviceToken(ctx, &principal.UserID, id); err != nil {
		var appErr *apperr.AppError
		if errors.As(MapToAppError(err), &appErr) {
			apperr.RespondError(w, reqID, appErr)
		} else {
			apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleVerifyDeviceToken(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewValidationError("id", "Invalid UUID format"))
		return
	}

	var body struct {
		DeviceToken string `json:"device_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	// Resolve tenant context: principal token if present, else X-Tenant-ID header
	ctx := r.Context()
	var tenantID uuid.UUID
	if principal, ok := auth.PrincipalFromContext(ctx); ok && principal.TenantID != uuid.Nil {
		tenantID = principal.TenantID
	} else if tenantHdr := r.Header.Get("X-Tenant-ID"); tenantHdr != "" {
		if parsed, err := uuid.Parse(tenantHdr); err == nil {
			tenantID = parsed
			ctx = postgres.WithTenantID(ctx, tenantID)
		}
	}

	if tenantID == uuid.Nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Missing required tenant context or X-Tenant-ID header"))
		return
	}

	terminal, err := h.service.VerifyDeviceToken(ctx, id, body.DeviceToken)
	if err != nil {
		var appErr *apperr.AppError
		if errors.As(MapToAppError(err), &appErr) {
			apperr.RespondError(w, reqID, appErr)
		} else {
			apperr.RespondError(w, reqID, apperr.NewInternalError(err.Error()))
		}
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]any{
		"valid":    true,
		"terminal": terminal,
	})
}
