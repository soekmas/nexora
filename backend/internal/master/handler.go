package master

import (
	"encoding/json"
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

// Handler exposes Master Data & Catalog capabilities via HTTP endpoints.
type Handler struct {
	service Service
	logger  *slog.Logger
}

// NewHandler constructs a new Master Data HTTP Handler.
func NewHandler(service Service, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		service: service,
		logger:  logger,
	}
}

// Routes returns the canonical domain router mounted at /api/v1/master.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Group(func(protected chi.Router) {
		// Product routes
		protected.Post("/products", h.handleCreateProduct)
		protected.Get("/products", h.handleListProducts)
		protected.Get("/products/{id}", h.handleGetProduct)
		protected.Patch("/products/{id}", h.handleUpdateProduct)
		protected.Post("/products/{id}/activate", h.handleActivateProduct)
		protected.Post("/products/{id}/deactivate", h.handleDeactivateProduct)
		protected.Get("/products/{id}/barcodes", h.handleListBarcodes)
		protected.Post("/products/{id}/barcodes", h.handleCreateBarcode)
		protected.Get("/products/lookup/barcode/{barcode}", h.handleLookupBarcode)
		protected.Post("/products/{id}/variants", h.handleCreateVariant)
		protected.Get("/products/{id}/variants", h.handleListVariants)
		protected.Post("/products/{id}/serials", h.handleCreateSerialNumber)
		protected.Get("/products/{id}/serials", h.handleListSerialNumbers)
		protected.Get("/serials/{serial_no}", h.handleGetSerialNumber)

		// Brand routes
		protected.Post("/brands", h.handleCreateBrand)
		protected.Get("/brands", h.handleListBrands)
		protected.Get("/brands/{id}", h.handleGetBrand)
		protected.Patch("/brands/{id}", h.handleUpdateBrand)

		// Category routes
		protected.Post("/categories", h.handleCreateCategory)
		protected.Get("/categories", h.handleListCategories)
		protected.Get("/categories/{id}", h.handleGetCategory)
		protected.Patch("/categories/{id}", h.handleUpdateCategory)

		// UOM routes
		protected.Post("/uoms", h.handleCreateUOM)
		protected.Get("/uoms", h.handleListUOMs)
		protected.Get("/uoms/{id}", h.handleGetUOM)
	})

	return r
}

// ProductAliasRoutes returns the compatibility alias router mounted at /api/v1/products (DEC-B1-013).
func (h *Handler) ProductAliasRoutes() chi.Router {
	r := chi.NewRouter()

	r.Group(func(protected chi.Router) {
		protected.Post("/", h.handleCreateProduct)
		protected.Get("/", h.handleListProducts)
		protected.Get("/{id}", h.handleGetProduct)
		protected.Patch("/{id}", h.handleUpdateProduct)
		protected.Post("/{id}/activate", h.handleActivateProduct)
		protected.Post("/{id}/deactivate", h.handleDeactivateProduct)
		protected.Get("/{id}/barcodes", h.handleListBarcodes)
		protected.Post("/{id}/barcodes", h.handleCreateBarcode)
		protected.Get("/lookup/barcode/{barcode}", h.handleLookupBarcode)
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

func toProductResponse(p *Product, variants []ProductVariant) ProductResponse {
	vResponses := make([]ProductVariantResponse, 0, len(variants))
	for _, v := range variants {
		vResponses = append(vResponses, ProductVariantResponse{
			ID:         v.ID,
			SKU:        v.SKU,
			Name:       v.Name,
			ProductID:  v.ProductID,
			Active:     v.Status == StatusActive,
			Status:     v.Status,
			Attributes: v.Attributes,
			CreatedAt:  v.CreatedAt,
			UpdatedAt:  v.UpdatedAt,
		})
	}

	return ProductResponse{
		ID:          p.ID,
		SKU:         p.SKU,
		Name:        p.Name,
		BrandID:     p.BrandID,
		CategoryID:  p.CategoryID,
		BaseUOMID:   p.BaseUOMID,
		UOMID:       p.BaseUOMID,
		CompanyID:   p.CompanyID,
		ProductType: p.ProductType,
		TrackSerial: p.TrackSerial,
		TrackLot:    p.TrackLot,
		TaxCategory: p.TaxCategory,
		Status:      p.Status,
		Active:      p.Status == StatusActive,
		Version:     p.Version,
		Variants:    vResponses,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

// ============================================================================
// Product Handlers
// ============================================================================

func (h *Handler) handleCreateProduct(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "product.create"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	var req CreateProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	p, err := h.service.CreateProduct(ctx, &principal.UserID, req)
	if err != nil {
		appErr := MapToAppError(err)
		apperr.RespondError(w, reqID, appErr)
		return
	}

	// Fetch created variants if any
	variants, _ := h.service.ListVariants(ctx, p.ID)
	resp := toProductResponse(p, variants)
	h.respondJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleGetProduct(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "product.read"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid product ID"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	details, err := h.service.GetProduct(ctx, id)
	if err != nil {
		appErr := MapToAppError(err)
		apperr.RespondError(w, reqID, appErr)
		return
	}

	resp := toProductResponse(&details.Product, details.Variants)
	h.respondJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleUpdateProduct(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "product.update"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid product ID"))
		return
	}

	var req UpdateProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	p, err := h.service.UpdateProduct(ctx, &principal.UserID, id, req)
	if err != nil {
		appErr := MapToAppError(err)
		apperr.RespondError(w, reqID, appErr)
		return
	}

	variants, _ := h.service.ListVariants(ctx, p.ID)
	resp := toProductResponse(p, variants)
	h.respondJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleActivateProduct(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "product.activate"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid product ID"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	p, err := h.service.ActivateProduct(ctx, &principal.UserID, id)
	if err != nil {
		appErr := MapToAppError(err)
		apperr.RespondError(w, reqID, appErr)
		return
	}

	variants, _ := h.service.ListVariants(ctx, p.ID)
	resp := toProductResponse(p, variants)
	h.respondJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleDeactivateProduct(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "product.deactivate"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid product ID"))
		return
	}

	var req ReasonRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	p, err := h.service.DeactivateProduct(ctx, &principal.UserID, id, req.Reason)
	if err != nil {
		appErr := MapToAppError(err)
		apperr.RespondError(w, reqID, appErr)
		return
	}

	variants, _ := h.service.ListVariants(ctx, p.ID)
	resp := toProductResponse(p, variants)
	h.respondJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleListProducts(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "product.read"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	page, pageSize, offset := parsePagination(r)
	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	products, total, err := h.service.ListProducts(ctx, pageSize, offset)
	if err != nil {
		appErr := MapToAppError(err)
		apperr.RespondError(w, reqID, appErr)
		return
	}

	items := make([]ProductResponse, 0, len(products))
	for i := range products {
		p := &products[i]
		variants, _ := h.service.ListVariants(ctx, p.ID)
		items = append(items, toProductResponse(p, variants))
	}

	resp := ProductListResponse{
		Items:  items,
		Meta:   makePaginationMeta(page, pageSize, total),
		Total:  total,
		Limit:  pageSize,
		Offset: offset,
	}
	h.respondJSON(w, http.StatusOK, resp)
}

// ============================================================================
// Barcode Handlers
// ============================================================================

func (h *Handler) handleCreateBarcode(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "product.barcode.manage"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	idStr := chi.URLParam(r, "id")
	productID, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid product ID"))
		return
	}

	var req CreateBarcodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	b, err := h.service.CreateBarcode(ctx, &principal.UserID, productID, req)
	if err != nil {
		appErr := MapToAppError(err)
		apperr.RespondError(w, reqID, appErr)
		return
	}

	resp := BarcodeResponse{
		ID:               b.ID,
		Barcode:          b.Barcode,
		Type:             b.BarcodeType,
		ProductID:        b.ProductID,
		ProductVariantID: b.VariantID,
		IsPrimary:        b.IsPrimary,
		Status:           b.Status,
	}
	h.respondJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleListBarcodes(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "product.barcode.read"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	idStr := chi.URLParam(r, "id")
	productID, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid product ID"))
		return
	}

	page, pageSize, offset := parsePagination(r)
	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	barcodes, total, err := h.service.ListBarcodes(ctx, productID, pageSize, offset)
	if err != nil {
		appErr := MapToAppError(err)
		apperr.RespondError(w, reqID, appErr)
		return
	}

	items := make([]BarcodeResponse, 0, len(barcodes))
	for _, b := range barcodes {
		items = append(items, BarcodeResponse{
			ID:               b.ID,
			Barcode:          b.Barcode,
			Type:             b.BarcodeType,
			ProductID:        b.ProductID,
			ProductVariantID: b.VariantID,
			IsPrimary:        b.IsPrimary,
			Status:           b.Status,
		})
	}

	resp := BarcodeListResponse{
		Items:  items,
		Meta:   makePaginationMeta(page, pageSize, total),
		Total:  total,
		Limit:  pageSize,
		Offset: offset,
	}
	h.respondJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleLookupBarcode(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "product.read"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	barcode := chi.URLParam(r, "barcode")
	if barcode == "" {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Barcode parameter required"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	details, err := h.service.LookupBarcode(ctx, barcode)
	if err != nil {
		appErr := MapToAppError(err)
		apperr.RespondError(w, reqID, appErr)
		return
	}

	resp := toProductResponse(&details.Product, details.Variants)
	h.respondJSON(w, http.StatusOK, resp)
}

// ============================================================================
// Variant Handlers
// ============================================================================

func (h *Handler) handleCreateVariant(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "product.update"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	idStr := chi.URLParam(r, "id")
	productID, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid product ID"))
		return
	}

	var req CreateVariantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	v, err := h.service.CreateVariant(ctx, &principal.UserID, productID, req)
	if err != nil {
		appErr := MapToAppError(err)
		apperr.RespondError(w, reqID, appErr)
		return
	}

	resp := ProductVariantResponse{
		ID:         v.ID,
		SKU:        v.SKU,
		Name:       v.Name,
		ProductID:  v.ProductID,
		Active:     v.Status == StatusActive,
		Status:     v.Status,
		Attributes: v.Attributes,
		CreatedAt:  v.CreatedAt,
		UpdatedAt:  v.UpdatedAt,
	}
	h.respondJSON(w, http.StatusCreated, resp)
}

func (h *Handler) handleListVariants(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "product.read"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	idStr := chi.URLParam(r, "id")
	productID, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid product ID"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	variants, err := h.service.ListVariants(ctx, productID)
	if err != nil {
		appErr := MapToAppError(err)
		apperr.RespondError(w, reqID, appErr)
		return
	}

	items := make([]ProductVariantResponse, 0, len(variants))
	for _, v := range variants {
		items = append(items, ProductVariantResponse{
			ID:         v.ID,
			SKU:        v.SKU,
			Name:       v.Name,
			ProductID:  v.ProductID,
			Active:     v.Status == StatusActive,
			Status:     v.Status,
			Attributes: v.Attributes,
			CreatedAt:  v.CreatedAt,
			UpdatedAt:  v.UpdatedAt,
		})
	}
	h.respondJSON(w, http.StatusOK, map[string]any{
		"items": items,
	})
}

// ============================================================================
// Brand Handlers
// ============================================================================

func (h *Handler) handleCreateBrand(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "brand.create"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	var req CreateBrandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	b, err := h.service.CreateBrand(ctx, &principal.UserID, req)
	if err != nil {
		appErr := MapToAppError(err)
		apperr.RespondError(w, reqID, appErr)
		return
	}

	h.respondJSON(w, http.StatusCreated, b)
}

func (h *Handler) handleGetBrand(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "brand.read"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid brand ID"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	b, err := h.service.GetBrand(ctx, id)
	if err != nil {
		appErr := MapToAppError(err)
		apperr.RespondError(w, reqID, appErr)
		return
	}

	h.respondJSON(w, http.StatusOK, b)
}

func (h *Handler) handleUpdateBrand(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "brand.update"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid brand ID"))
		return
	}

	var req UpdateBrandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	b, err := h.service.UpdateBrand(ctx, &principal.UserID, id, req)
	if err != nil {
		appErr := MapToAppError(err)
		apperr.RespondError(w, reqID, appErr)
		return
	}

	h.respondJSON(w, http.StatusOK, b)
}

func (h *Handler) handleListBrands(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "brand.read"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	page, pageSize, offset := parsePagination(r)
	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	items, total, err := h.service.ListBrands(ctx, pageSize, offset)
	if err != nil {
		appErr := MapToAppError(err)
		apperr.RespondError(w, reqID, appErr)
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"meta":   makePaginationMeta(page, pageSize, total),
		"total":  total,
		"limit":  pageSize,
		"offset": offset,
	})
}

// ============================================================================
// Category Handlers
// ============================================================================

func (h *Handler) handleCreateCategory(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "category.create"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	var req CreateCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	c, err := h.service.CreateCategory(ctx, &principal.UserID, req)
	if err != nil {
		appErr := MapToAppError(err)
		apperr.RespondError(w, reqID, appErr)
		return
	}

	h.respondJSON(w, http.StatusCreated, c)
}

func (h *Handler) handleGetCategory(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "category.read"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid category ID"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	c, err := h.service.GetCategory(ctx, id)
	if err != nil {
		appErr := MapToAppError(err)
		apperr.RespondError(w, reqID, appErr)
		return
	}

	h.respondJSON(w, http.StatusOK, c)
}

func (h *Handler) handleUpdateCategory(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "category.update"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid category ID"))
		return
	}

	var req UpdateCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	c, err := h.service.UpdateCategory(ctx, &principal.UserID, id, req)
	if err != nil {
		appErr := MapToAppError(err)
		apperr.RespondError(w, reqID, appErr)
		return
	}

	h.respondJSON(w, http.StatusOK, c)
}

func (h *Handler) handleListCategories(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "category.read"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	page, pageSize, offset := parsePagination(r)
	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	items, total, err := h.service.ListCategories(ctx, pageSize, offset)
	if err != nil {
		appErr := MapToAppError(err)
		apperr.RespondError(w, reqID, appErr)
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"meta":   makePaginationMeta(page, pageSize, total),
		"total":  total,
		"limit":  pageSize,
		"offset": offset,
	})
}

// ============================================================================
// UOM Handlers
// ============================================================================

func (h *Handler) handleCreateUOM(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "uom.create"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	var req CreateUOMRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	u, err := h.service.CreateUOM(ctx, &principal.UserID, req)
	if err != nil {
		appErr := MapToAppError(err)
		apperr.RespondError(w, reqID, appErr)
		return
	}

	h.respondJSON(w, http.StatusCreated, u)
}

func (h *Handler) handleGetUOM(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "uom.read"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid UOM ID"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	u, err := h.service.GetUOM(ctx, id)
	if err != nil {
		appErr := MapToAppError(err)
		apperr.RespondError(w, reqID, appErr)
		return
	}

	h.respondJSON(w, http.StatusOK, u)
}

func (h *Handler) handleListUOMs(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "uom.read"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	page, pageSize, offset := parsePagination(r)
	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	items, total, err := h.service.ListUOMs(ctx, pageSize, offset)
	if err != nil {
		appErr := MapToAppError(err)
		apperr.RespondError(w, reqID, appErr)
		return
	}

	h.respondJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"meta":   makePaginationMeta(page, pageSize, total),
		"total":  total,
		"limit":  pageSize,
		"offset": offset,
	})
}

// ============================================================================
// Serial Number Handlers (DEC-B1-011)
// ============================================================================

func (h *Handler) handleCreateSerialNumber(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "product.update"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	idStr := chi.URLParam(r, "id")
	productID, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid product ID"))
		return
	}

	var req CreateSerialNumberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Malformed request payload"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	sn, err := h.service.CreateSerialNumber(ctx, &principal.UserID, productID, req.SerialNo, req.VariantID)
	if err != nil {
		appErr := MapToAppError(err)
		apperr.RespondError(w, reqID, appErr)
		return
	}

	resp := SerialNumberResponse{
		ID:        sn.ID,
		ProductID: sn.ProductID,
		VariantID: sn.VariantID,
		SerialNo:  sn.SerialNo,
		Status:    sn.Status,
		CreatedAt: sn.CreatedAt,
	}
	h.respondJSON(w, http.StatusCreated, resp)
}

func (h *Handler) handleGetSerialNumber(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "product.read"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	serialNo := chi.URLParam(r, "serial_no")
	if serialNo == "" {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Serial number required"))
		return
	}

	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	sn, err := h.service.GetSerialNumber(ctx, serialNo)
	if err != nil {
		appErr := MapToAppError(err)
		apperr.RespondError(w, reqID, appErr)
		return
	}

	resp := SerialNumberResponse{
		ID:        sn.ID,
		ProductID: sn.ProductID,
		VariantID: sn.VariantID,
		SerialNo:  sn.SerialNo,
		Status:    sn.Status,
		CreatedAt: sn.CreatedAt,
	}
	h.respondJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleListSerialNumbers(w http.ResponseWriter, r *http.Request) {
	reqID := platformmw.GetRequestID(r.Context())
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil {
		apperr.RespondError(w, reqID, apperr.NewUnauthorizedError("Authentication required"))
		return
	}

	if permErr := checkPermission(principal, "product.read"); permErr != nil {
		apperr.RespondError(w, reqID, permErr)
		return
	}

	idStr := chi.URLParam(r, "id")
	productID, err := uuid.Parse(idStr)
	if err != nil {
		apperr.RespondError(w, reqID, apperr.NewBadRequestError("Invalid product ID"))
		return
	}

	page, pageSize, offset := parsePagination(r)
	ctx := postgres.WithTenantID(r.Context(), principal.TenantID)
	serials, total, err := h.service.ListSerialNumbers(ctx, productID, pageSize, offset)
	if err != nil {
		appErr := MapToAppError(err)
		apperr.RespondError(w, reqID, appErr)
		return
	}

	items := make([]SerialNumberResponse, 0, len(serials))
	for _, sn := range serials {
		items = append(items, SerialNumberResponse{
			ID:        sn.ID,
			ProductID: sn.ProductID,
			VariantID: sn.VariantID,
			SerialNo:  sn.SerialNo,
			Status:    sn.Status,
			CreatedAt: sn.CreatedAt,
		})
	}

	resp := SerialNumberListResponse{
		Items:  items,
		Meta:   makePaginationMeta(page, pageSize, total),
		Total:  total,
		Limit:  pageSize,
		Offset: offset,
	}
	h.respondJSON(w, http.StatusOK, resp)
}
