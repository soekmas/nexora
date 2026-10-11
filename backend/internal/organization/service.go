package organization

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/platform/audit"
	"example.com/nexora/backend/internal/platform/outbox"
	"example.com/nexora/backend/internal/platform/postgres"
)

// Service provides organization hierarchy management capabilities.
type Service interface {
	// Company
	CreateCompany(ctx context.Context, actorID *uuid.UUID, req CreateCompanyRequest) (*Company, error)
	GetCompany(ctx context.Context, id uuid.UUID) (*Company, error)
	UpdateCompany(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req UpdateCompanyRequest) (*Company, error)
	ListCompanies(ctx context.Context, limit, offset int) ([]Company, int64, error)

	// Region
	CreateRegion(ctx context.Context, actorID *uuid.UUID, req CreateRegionRequest) (*Region, error)
	GetRegion(ctx context.Context, id uuid.UUID) (*Region, error)
	UpdateRegion(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req UpdateRegionRequest) (*Region, error)
	ListRegions(ctx context.Context, companyID *uuid.UUID, limit, offset int) ([]Region, int64, error)

	// Store
	CreateStore(ctx context.Context, actorID *uuid.UUID, req CreateStoreRequest) (*Store, error)
	GetStore(ctx context.Context, id uuid.UUID) (*Store, error)
	UpdateStore(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req UpdateStoreRequest) (*Store, error)
	ListStores(ctx context.Context, companyID *uuid.UUID, limit, offset int) ([]Store, int64, error)

	// Warehouse
	CreateWarehouse(ctx context.Context, actorID *uuid.UUID, req CreateWarehouseRequest) (*Warehouse, error)
	GetWarehouse(ctx context.Context, id uuid.UUID) (*Warehouse, error)
	UpdateWarehouse(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req UpdateWarehouseRequest) (*Warehouse, error)
	ListWarehouses(ctx context.Context, companyID *uuid.UUID, storeID *uuid.UUID, limit, offset int) ([]Warehouse, int64, error)

	// Terminal & Device Tokens
	RegisterTerminal(ctx context.Context, actorID *uuid.UUID, req RegisterTerminalRequest) (*TerminalRegistrationResult, error)
	GetTerminal(ctx context.Context, id uuid.UUID) (*Terminal, error)
	UpdateTerminal(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req UpdateTerminalRequest) (*Terminal, error)
	ListTerminals(ctx context.Context, storeID *uuid.UUID, limit, offset int) ([]Terminal, int64, error)

	RotateDeviceToken(ctx context.Context, actorID *uuid.UUID, terminalID uuid.UUID) (*TerminalRegistrationResult, error)
	RevokeDeviceToken(ctx context.Context, actorID *uuid.UUID, terminalID uuid.UUID) error
	VerifyDeviceToken(ctx context.Context, terminalID uuid.UUID, token string) (*Terminal, error)

	// Cross-domain validation
	ValidateScopeEntity(ctx context.Context, scopeType string, entityID uuid.UUID) (bool, error)
}

// DomainService implements Service.
type DomainService struct {
	store        OrgStore
	txManager    postgres.TxManager
	auditStore   audit.Store
	outboxWriter outbox.Writer
	logger       *slog.Logger
}

// NewService constructs a new DomainService.
func NewService(
	store OrgStore,
	txManager postgres.TxManager,
	auditStore audit.Store,
	outboxWriter outbox.Writer,
	logger *slog.Logger,
) *DomainService {
	if logger == nil {
		logger = slog.Default()
	}
	return &DomainService{
		store:        store,
		txManager:    txManager,
		auditStore:   auditStore,
		outboxWriter: outboxWriter,
		logger:       logger,
	}
}

// ============================================================================
// Company Service
// ============================================================================

// CreateCompany creates a new operating company under the tenant.
func (s *DomainService) CreateCompany(ctx context.Context, actorID *uuid.UUID, req CreateCompanyRequest) (*Company, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	req.Code = strings.TrimSpace(req.Code)
	if req.Code == "" {
		return nil, fmt.Errorf("%w: company code is required", ErrValidation)
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, fmt.Errorf("%w: company name is required", ErrValidation)
	}
	if req.BaseCurrency == "" {
		req.BaseCurrency = "IDR"
	}
	if req.Timezone == "" {
		req.Timezone = "Asia/Jakarta"
	}

	c := &Company{
		ID:           uuid.New(),
		TenantID:     pc.TenantID,
		Code:         req.Code,
		Name:         req.Name,
		LegalName:    req.LegalName,
		TaxID:        req.TaxID,
		BaseCurrency: req.BaseCurrency,
		Timezone:     req.Timezone,
		Status:       StatusActive,
		CreatedBy:    actorID,
		Version:      1,
	}

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		if err := s.store.CreateCompany(txCtx, c); err != nil {
			return err
		}

		if s.auditStore != nil {
			meta, _ := json.Marshal(map[string]any{
				"code":          c.Code,
				"name":          c.Name,
				"base_currency": c.BaseCurrency,
				"timezone":      c.Timezone,
			})
			resType := "company"
			_ = s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     c.TenantID,
				UserID:       actorID,
				Module:       "organization",
				Action:       AuditActionCompanyCreated,
				ResourceType: &resType,
				ResourceID:   &c.ID,
				Status:       audit.StatusSuccess,
				Metadata:     meta,
				CreatedAt:    time.Now().UTC(),
			}, nil)
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"company_id": c.ID,
				"code":       c.Code,
				"name":       c.Name,
			}
			if evt, err := outbox.NewEvent(c.TenantID, "company", c.ID, EventCompanyCreated, 1, payload); err == nil {
				_ = s.outboxWriter.Write(txCtx, evt)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return c, nil
}

// GetCompany retrieves a company by its primary key.
func (s *DomainService) GetCompany(ctx context.Context, id uuid.UUID) (*Company, error) {
	var c *Company
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		c, err = s.store.GetCompanyByID(txCtx, id)
		return err
	})
	return c, err
}

// UpdateCompany modifies company properties with optimistic locking.
func (s *DomainService) UpdateCompany(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req UpdateCompanyRequest) (*Company, error) {
	var updated *Company
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		c, err := s.store.GetCompanyByID(txCtx, id)
		if err != nil {
			return err
		}

		if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
			c.Name = strings.TrimSpace(*req.Name)
		}
		if req.LegalName != nil {
			c.LegalName = req.LegalName
		}
		if req.TaxID != nil {
			c.TaxID = req.TaxID
		}
		if req.BaseCurrency != nil && strings.TrimSpace(*req.BaseCurrency) != "" {
			c.BaseCurrency = strings.TrimSpace(*req.BaseCurrency)
		}
		if req.Timezone != nil && strings.TrimSpace(*req.Timezone) != "" {
			c.Timezone = strings.TrimSpace(*req.Timezone)
		}
		if req.Status != nil {
			newStatus := strings.ToUpper(strings.TrimSpace(*req.Status))
			if newStatus != StatusActive && newStatus != StatusInactive {
				return fmt.Errorf("%w: status must be ACTIVE or INACTIVE", ErrInvalidStatusTransition)
			}
			c.Status = newStatus
		}
		c.UpdatedBy = actorID

		if err := s.store.UpdateCompany(txCtx, c); err != nil {
			return err
		}
		updated = c

		if s.auditStore != nil {
			resType := "company"
			_ = s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     c.TenantID,
				UserID:       actorID,
				Module:       "organization",
				Action:       AuditActionCompanyUpdated,
				ResourceType: &resType,
				ResourceID:   &c.ID,
				Status:       audit.StatusSuccess,
				CreatedAt:    time.Now().UTC(),
			}, nil)
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"company_id": c.ID,
				"name":       c.Name,
				"status":     c.Status,
			}
			if evt, err := outbox.NewEvent(c.TenantID, "company", c.ID, EventCompanyUpdated, 1, payload); err == nil {
				_ = s.outboxWriter.Write(txCtx, evt)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return updated, nil
}

// ListCompanies lists companies with pagination.
func (s *DomainService) ListCompanies(ctx context.Context, limit, offset int) ([]Company, int64, error) {
	var list []Company
	var total int64
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		list, total, err = s.store.ListCompanies(txCtx, limit, offset)
		return err
	})
	return list, total, err
}

// ============================================================================
// Region Service
// ============================================================================

// CreateRegion creates a new regional territory under a company.
func (s *DomainService) CreateRegion(ctx context.Context, actorID *uuid.UUID, req CreateRegionRequest) (*Region, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	req.Code = strings.TrimSpace(req.Code)
	if req.Code == "" {
		return nil, fmt.Errorf("%w: region code is required", ErrValidation)
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, fmt.Errorf("%w: region name is required", ErrValidation)
	}
	if req.CompanyID == uuid.Nil {
		return nil, fmt.Errorf("%w: company_id is required", ErrValidation)
	}

	r := &Region{
		ID:        uuid.New(),
		TenantID:  pc.TenantID,
		CompanyID: req.CompanyID,
		Code:      req.Code,
		Name:      req.Name,
		Status:    StatusActive,
		CreatedBy: actorID,
		Version:   1,
	}

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		company, err := s.store.GetCompanyByID(txCtx, req.CompanyID)
		if err != nil {
			if errors.Is(err, ErrCompanyNotFound) {
				return ErrParentNotFound
			}
			return err
		}
		if company.TenantID != pc.TenantID {
			return ErrParentTenantMismatch
		}

		if err := s.store.CreateRegion(txCtx, r); err != nil {
			return err
		}

		if s.auditStore != nil {
			resType := "region"
			_ = s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     r.TenantID,
				UserID:       actorID,
				Module:       "organization",
				Action:       AuditActionRegionCreated,
				ResourceType: &resType,
				ResourceID:   &r.ID,
				Status:       audit.StatusSuccess,
				CreatedAt:    time.Now().UTC(),
			}, nil)
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"region_id":  r.ID,
				"company_id": r.CompanyID,
				"code":       r.Code,
				"name":       r.Name,
			}
			if evt, err := outbox.NewEvent(r.TenantID, "region", r.ID, EventRegionCreated, 1, payload); err == nil {
				_ = s.outboxWriter.Write(txCtx, evt)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return r, nil
}

// GetRegion retrieves a region by its primary key.
func (s *DomainService) GetRegion(ctx context.Context, id uuid.UUID) (*Region, error) {
	var r *Region
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		r, err = s.store.GetRegionByID(txCtx, id)
		return err
	})
	return r, err
}

// UpdateRegion modifies region properties.
func (s *DomainService) UpdateRegion(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req UpdateRegionRequest) (*Region, error) {
	var updated *Region
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		r, err := s.store.GetRegionByID(txCtx, id)
		if err != nil {
			return err
		}

		if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
			r.Name = strings.TrimSpace(*req.Name)
		}
		if req.Status != nil {
			newStatus := strings.ToUpper(strings.TrimSpace(*req.Status))
			if newStatus != StatusActive && newStatus != StatusInactive {
				return fmt.Errorf("%w: status must be ACTIVE or INACTIVE", ErrInvalidStatusTransition)
			}
			r.Status = newStatus
		}
		r.UpdatedBy = actorID

		if err := s.store.UpdateRegion(txCtx, r); err != nil {
			return err
		}
		updated = r

		if s.auditStore != nil {
			resType := "region"
			_ = s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     r.TenantID,
				UserID:       actorID,
				Module:       "organization",
				Action:       AuditActionRegionUpdated,
				ResourceType: &resType,
				ResourceID:   &r.ID,
				Status:       audit.StatusSuccess,
				CreatedAt:    time.Now().UTC(),
			}, nil)
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"region_id": r.ID,
				"name":      r.Name,
				"status":    r.Status,
			}
			if evt, err := outbox.NewEvent(r.TenantID, "region", r.ID, EventRegionUpdated, 1, payload); err == nil {
				_ = s.outboxWriter.Write(txCtx, evt)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return updated, nil
}

// ListRegions queries regions, optionally filtered by company ID.
func (s *DomainService) ListRegions(ctx context.Context, companyID *uuid.UUID, limit, offset int) ([]Region, int64, error) {
	var list []Region
	var total int64
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		list, total, err = s.store.ListRegions(txCtx, companyID, limit, offset)
		return err
	})
	return list, total, err
}

// ============================================================================
// Store Service
// ============================================================================

// CreateStore creates a new retail store.
func (s *DomainService) CreateStore(ctx context.Context, actorID *uuid.UUID, req CreateStoreRequest) (*Store, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	req.Code = strings.TrimSpace(req.Code)
	if req.Code == "" {
		return nil, fmt.Errorf("%w: store code is required", ErrValidation)
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, fmt.Errorf("%w: store name is required", ErrValidation)
	}
	if req.CompanyID == uuid.Nil {
		return nil, fmt.Errorf("%w: company_id is required", ErrValidation)
	}
	if req.Timezone == "" {
		req.Timezone = "Asia/Jakarta"
	}

	st := &Store{
		ID:        uuid.New(),
		TenantID:  pc.TenantID,
		CompanyID: req.CompanyID,
		RegionID:  req.RegionID,
		Code:      req.Code,
		Name:      req.Name,
		Address:   req.Address,
		Timezone:  req.Timezone,
		Status:    StatusActive,
		CreatedBy: actorID,
		Version:   1,
	}

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		company, err := s.store.GetCompanyByID(txCtx, req.CompanyID)
		if err != nil {
			if errors.Is(err, ErrCompanyNotFound) {
				return ErrParentNotFound
			}
			return err
		}
		if company.TenantID != pc.TenantID {
			return ErrParentTenantMismatch
		}

		if req.RegionID != nil && *req.RegionID != uuid.Nil {
			region, err := s.store.GetRegionByID(txCtx, *req.RegionID)
			if err != nil {
				if errors.Is(err, ErrRegionNotFound) {
					return ErrParentNotFound
				}
				return err
			}
			if region.CompanyID != req.CompanyID {
				return ErrParentTenantMismatch
			}
		}

		if err := s.store.CreateStore(txCtx, st); err != nil {
			return err
		}

		if s.auditStore != nil {
			resType := "store"
			_ = s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     st.TenantID,
				UserID:       actorID,
				Module:       "organization",
				Action:       AuditActionStoreCreated,
				ResourceType: &resType,
				ResourceID:   &st.ID,
				Status:       audit.StatusSuccess,
				CreatedAt:    time.Now().UTC(),
			}, nil)
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"store_id":   st.ID,
				"company_id": st.CompanyID,
				"code":       st.Code,
				"name":       st.Name,
			}
			if evt, err := outbox.NewEvent(st.TenantID, "store", st.ID, EventStoreCreated, 1, payload); err == nil {
				_ = s.outboxWriter.Write(txCtx, evt)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return st, nil
}

// GetStore retrieves a store by primary key.
func (s *DomainService) GetStore(ctx context.Context, id uuid.UUID) (*Store, error) {
	var st *Store
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		st, err = s.store.GetStoreByID(txCtx, id)
		return err
	})
	return st, err
}

// UpdateStore modifies store properties.
func (s *DomainService) UpdateStore(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req UpdateStoreRequest) (*Store, error) {
	var updated *Store
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		st, err := s.store.GetStoreByID(txCtx, id)
		if err != nil {
			return err
		}

		if req.RegionID != nil {
			if *req.RegionID != uuid.Nil {
				region, err := s.store.GetRegionByID(txCtx, *req.RegionID)
				if err != nil {
					if errors.Is(err, ErrRegionNotFound) {
						return ErrParentNotFound
					}
					return err
				}
				if region.CompanyID != st.CompanyID {
					return ErrParentTenantMismatch
				}
				st.RegionID = req.RegionID
			} else {
				st.RegionID = nil
			}
		}

		if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
			st.Name = strings.TrimSpace(*req.Name)
		}
		if req.Address != nil {
			st.Address = req.Address
		}
		if req.Timezone != nil && strings.TrimSpace(*req.Timezone) != "" {
			st.Timezone = strings.TrimSpace(*req.Timezone)
		}

		action := AuditActionStoreUpdated
		event := EventStoreUpdated
		if req.Status != nil {
			newStatus := strings.ToUpper(strings.TrimSpace(*req.Status))
			if newStatus != StatusActive && newStatus != StatusInactive {
				return fmt.Errorf("%w: status must be ACTIVE or INACTIVE", ErrInvalidStatusTransition)
			}
			if st.Status != newStatus {
				if newStatus == StatusInactive {
					action = AuditActionStoreDisabled
					event = EventStoreDisabled
				} else {
					action = AuditActionStoreEnabled
					event = EventStoreEnabled
				}
			}
			st.Status = newStatus
		}
		st.UpdatedBy = actorID

		if err := s.store.UpdateStore(txCtx, st); err != nil {
			return err
		}
		updated = st

		if s.auditStore != nil {
			resType := "store"
			_ = s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     st.TenantID,
				UserID:       actorID,
				Module:       "organization",
				Action:       action,
				ResourceType: &resType,
				ResourceID:   &st.ID,
				Status:       audit.StatusSuccess,
				CreatedAt:    time.Now().UTC(),
			}, nil)
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"store_id": st.ID,
				"name":     st.Name,
				"status":   st.Status,
			}
			if evt, err := outbox.NewEvent(st.TenantID, "store", st.ID, event, 1, payload); err == nil {
				_ = s.outboxWriter.Write(txCtx, evt)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return updated, nil
}

// ListStores queries stores, optionally filtered by company ID.
func (s *DomainService) ListStores(ctx context.Context, companyID *uuid.UUID, limit, offset int) ([]Store, int64, error) {
	var list []Store
	var total int64
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		list, total, err = s.store.ListStores(txCtx, companyID, limit, offset)
		return err
	})
	return list, total, err
}

// ============================================================================
// Warehouse Service
// ============================================================================

// CreateWarehouse creates a new storage facility.
func (s *DomainService) CreateWarehouse(ctx context.Context, actorID *uuid.UUID, req CreateWarehouseRequest) (*Warehouse, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	req.Code = strings.TrimSpace(req.Code)
	if req.Code == "" {
		return nil, fmt.Errorf("%w: warehouse code is required", ErrValidation)
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, fmt.Errorf("%w: warehouse name is required", ErrValidation)
	}
	if req.CompanyID == uuid.Nil {
		return nil, fmt.Errorf("%w: company_id is required", ErrValidation)
	}
	if req.WarehouseType == "" {
		req.WarehouseType = WarehouseTypeStore
	}

	w := &Warehouse{
		ID:            uuid.New(),
		TenantID:      pc.TenantID,
		CompanyID:     req.CompanyID,
		StoreID:       req.StoreID,
		Code:          req.Code,
		Name:          req.Name,
		WarehouseType: req.WarehouseType,
		Status:        StatusActive,
		CreatedBy:     actorID,
		Version:       1,
	}

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		company, err := s.store.GetCompanyByID(txCtx, req.CompanyID)
		if err != nil {
			if errors.Is(err, ErrCompanyNotFound) {
				return ErrParentNotFound
			}
			return err
		}
		if company.TenantID != pc.TenantID {
			return ErrParentTenantMismatch
		}

		if req.StoreID != nil && *req.StoreID != uuid.Nil {
			store, err := s.store.GetStoreByID(txCtx, *req.StoreID)
			if err != nil {
				if errors.Is(err, ErrStoreNotFound) {
					return ErrParentNotFound
				}
				return err
			}
			if store.CompanyID != req.CompanyID {
				return ErrParentTenantMismatch
			}
		}

		if err := s.store.CreateWarehouse(txCtx, w); err != nil {
			return err
		}

		if s.auditStore != nil {
			resType := "warehouse"
			_ = s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     w.TenantID,
				UserID:       actorID,
				Module:       "organization",
				Action:       AuditActionWarehouseCreated,
				ResourceType: &resType,
				ResourceID:   &w.ID,
				Status:       audit.StatusSuccess,
				CreatedAt:    time.Now().UTC(),
			}, nil)
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"warehouse_id": w.ID,
				"company_id":   w.CompanyID,
				"code":         w.Code,
				"name":         w.Name,
			}
			if evt, err := outbox.NewEvent(w.TenantID, "warehouse", w.ID, EventWarehouseCreated, 1, payload); err == nil {
				_ = s.outboxWriter.Write(txCtx, evt)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return w, nil
}

// GetWarehouse retrieves a warehouse by primary key.
func (s *DomainService) GetWarehouse(ctx context.Context, id uuid.UUID) (*Warehouse, error) {
	var w *Warehouse
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		w, err = s.store.GetWarehouseByID(txCtx, id)
		return err
	})
	return w, err
}

// UpdateWarehouse modifies warehouse attributes.
func (s *DomainService) UpdateWarehouse(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req UpdateWarehouseRequest) (*Warehouse, error) {
	var updated *Warehouse
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		w, err := s.store.GetWarehouseByID(txCtx, id)
		if err != nil {
			return err
		}

		if req.StoreID != nil {
			if *req.StoreID != uuid.Nil {
				store, err := s.store.GetStoreByID(txCtx, *req.StoreID)
				if err != nil {
					if errors.Is(err, ErrStoreNotFound) {
						return ErrParentNotFound
					}
					return err
				}
				if store.CompanyID != w.CompanyID {
					return ErrParentTenantMismatch
				}
				w.StoreID = req.StoreID
			} else {
				w.StoreID = nil
			}
		}

		if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
			w.Name = strings.TrimSpace(*req.Name)
		}
		if req.WarehouseType != nil && strings.TrimSpace(*req.WarehouseType) != "" {
			w.WarehouseType = strings.TrimSpace(*req.WarehouseType)
		}
		if req.Status != nil {
			newStatus := strings.ToUpper(strings.TrimSpace(*req.Status))
			if newStatus != StatusActive && newStatus != StatusInactive {
				return fmt.Errorf("%w: status must be ACTIVE or INACTIVE", ErrInvalidStatusTransition)
			}
			w.Status = newStatus
		}
		w.UpdatedBy = actorID

		if err := s.store.UpdateWarehouse(txCtx, w); err != nil {
			return err
		}
		updated = w

		if s.auditStore != nil {
			resType := "warehouse"
			_ = s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     w.TenantID,
				UserID:       actorID,
				Module:       "organization",
				Action:       AuditActionWarehouseUpdated,
				ResourceType: &resType,
				ResourceID:   &w.ID,
				Status:       audit.StatusSuccess,
				CreatedAt:    time.Now().UTC(),
			}, nil)
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"warehouse_id": w.ID,
				"name":         w.Name,
				"status":       w.Status,
			}
			if evt, err := outbox.NewEvent(w.TenantID, "warehouse", w.ID, EventWarehouseUpdated, 1, payload); err == nil {
				_ = s.outboxWriter.Write(txCtx, evt)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return updated, nil
}

// ListWarehouses lists warehouses, optionally filtered by company or store.
func (s *DomainService) ListWarehouses(ctx context.Context, companyID *uuid.UUID, storeID *uuid.UUID, limit, offset int) ([]Warehouse, int64, error) {
	var list []Warehouse
	var total int64
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		list, total, err = s.store.ListWarehouses(txCtx, companyID, storeID, limit, offset)
		return err
	})
	return list, total, err
}

// ============================================================================
// Terminal & Device Token Service
// ============================================================================

// RegisterTerminal creates a new terminal record and issues an initial device token.
func (s *DomainService) RegisterTerminal(ctx context.Context, actorID *uuid.UUID, req RegisterTerminalRequest) (*TerminalRegistrationResult, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	req.Code = strings.TrimSpace(req.Code)
	if req.Code == "" {
		return nil, fmt.Errorf("%w: terminal code is required", ErrValidation)
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, fmt.Errorf("%w: terminal name is required", ErrValidation)
	}
	if req.CompanyID == uuid.Nil {
		return nil, fmt.Errorf("%w: company_id is required", ErrValidation)
	}
	if req.StoreID == uuid.Nil {
		return nil, fmt.Errorf("%w: store_id is required", ErrValidation)
	}

	// Generate device token per DEC-B1-004 & DEC-B1-007
	plainToken, tokenHash, err := GenerateDeviceToken()
	if err != nil {
		return nil, err
	}

	t := &Terminal{
		ID:               uuid.New(),
		TenantID:         pc.TenantID,
		CompanyID:        req.CompanyID,
		StoreID:          req.StoreID,
		Code:             req.Code,
		Name:             req.Name,
		DeviceIdentifier: &tokenHash, // Storing SHA-256 hash
		Status:           TerminalStatusActive,
		CreatedBy:        actorID,
		Version:          1,
	}

	err = s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		company, err := s.store.GetCompanyByID(txCtx, req.CompanyID)
		if err != nil {
			if errors.Is(err, ErrCompanyNotFound) {
				return ErrParentNotFound
			}
			return err
		}
		if company.TenantID != pc.TenantID {
			return ErrParentTenantMismatch
		}

		store, err := s.store.GetStoreByID(txCtx, req.StoreID)
		if err != nil {
			if errors.Is(err, ErrStoreNotFound) {
				return ErrParentNotFound
			}
			return err
		}
		if store.CompanyID != req.CompanyID {
			return ErrParentTenantMismatch
		}

		if err := s.store.CreateTerminal(txCtx, t); err != nil {
			return err
		}

		if s.auditStore != nil {
			resType := "terminal"
			_ = s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     t.TenantID,
				UserID:       actorID,
				Module:       "organization",
				Action:       AuditActionTerminalRegistered,
				ResourceType: &resType,
				ResourceID:   &t.ID,
				Status:       audit.StatusSuccess,
				CreatedAt:    time.Now().UTC(),
			}, nil)

			devResType := "device"
			_ = s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     t.TenantID,
				UserID:       actorID,
				Module:       "organization",
				Action:       AuditActionDeviceRegistered,
				ResourceType: &devResType,
				ResourceID:   &t.ID,
				Status:       audit.StatusSuccess,
				CreatedAt:    time.Now().UTC(),
			}, nil)
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"terminal_id": t.ID,
				"store_id":    t.StoreID,
				"code":        t.Code,
				"name":        t.Name,
			}
			if evt, err := outbox.NewEvent(t.TenantID, "terminal", t.ID, EventTerminalRegistered, 1, payload); err == nil {
				_ = s.outboxWriter.Write(txCtx, evt)
			}
			if devEvt, err := outbox.NewEvent(t.TenantID, "device", t.ID, EventDeviceRegistered, 1, payload); err == nil {
				_ = s.outboxWriter.Write(txCtx, devEvt)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return &TerminalRegistrationResult{
		Terminal:    *t,
		DeviceToken: plainToken, // Returned ONCE upon registration
	}, nil
}

// GetTerminal retrieves a terminal by primary key.
func (s *DomainService) GetTerminal(ctx context.Context, id uuid.UUID) (*Terminal, error) {
	var t *Terminal
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		t, err = s.store.GetTerminalByID(txCtx, id)
		return err
	})
	return t, err
}

// UpdateTerminal modifies terminal name or status.
func (s *DomainService) UpdateTerminal(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req UpdateTerminalRequest) (*Terminal, error) {
	var updated *Terminal
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		t, err := s.store.GetTerminalByID(txCtx, id)
		if err != nil {
			return err
		}

		if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
			t.Name = strings.TrimSpace(*req.Name)
		}

		action := AuditActionTerminalRegistered
		event := EventTerminalUpdated
		if req.Status != nil {
			newStatus := strings.ToUpper(strings.TrimSpace(*req.Status))
			switch newStatus {
			case TerminalStatusActive:
				action = AuditActionTerminalEnabled
			case TerminalStatusDisabled, TerminalStatusInactive:
				action = AuditActionTerminalDisabled
				event = EventTerminalDisabled
			case TerminalStatusRetired:
				action = AuditActionTerminalRetired
				event = EventTerminalRetired
			case TerminalStatusMaintenance:
				action = AuditActionTerminalDisabled
			default:
				return fmt.Errorf("%w: status %s not allowed", ErrInvalidStatusTransition, newStatus)
			}
			t.Status = newStatus
		}
		t.UpdatedBy = actorID

		if err := s.store.UpdateTerminal(txCtx, t); err != nil {
			return err
		}
		updated = t

		if s.auditStore != nil {
			resType := "terminal"
			_ = s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     t.TenantID,
				UserID:       actorID,
				Module:       "organization",
				Action:       action,
				ResourceType: &resType,
				ResourceID:   &t.ID,
				Status:       audit.StatusSuccess,
				CreatedAt:    time.Now().UTC(),
			}, nil)
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"terminal_id": t.ID,
				"name":        t.Name,
				"status":      t.Status,
			}
			if evt, err := outbox.NewEvent(t.TenantID, "terminal", t.ID, event, 1, payload); err == nil {
				_ = s.outboxWriter.Write(txCtx, evt)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return updated, nil
}

// ListTerminals queries terminals, optionally filtered by store ID.
func (s *DomainService) ListTerminals(ctx context.Context, storeID *uuid.UUID, limit, offset int) ([]Terminal, int64, error) {
	var list []Terminal
	var total int64
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		list, total, err = s.store.ListTerminals(txCtx, storeID, limit, offset)
		return err
	})
	return list, total, err
}

// RotateDeviceToken rotates a terminal device token and returns the new plaintext token once.
func (s *DomainService) RotateDeviceToken(ctx context.Context, actorID *uuid.UUID, terminalID uuid.UUID) (*TerminalRegistrationResult, error) {
	plainToken, tokenHash, err := GenerateDeviceToken()
	if err != nil {
		return nil, err
	}

	var term *Terminal
	err = s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		t, err := s.store.GetTerminalByID(txCtx, terminalID)
		if err != nil {
			return err
		}
		if t.Status == TerminalStatusRetired {
			return ErrTerminalRetired
		}

		if err := s.store.UpdateTerminalDeviceHash(txCtx, terminalID, &tokenHash, actorID); err != nil {
			return err
		}
		t.DeviceIdentifier = &tokenHash
		term = t

		if s.auditStore != nil {
			devResType := "device"
			_ = s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     t.TenantID,
				UserID:       actorID,
				Module:       "organization",
				Action:       AuditActionDeviceRegistered,
				ResourceType: &devResType,
				ResourceID:   &t.ID,
				Status:       audit.StatusSuccess,
				CreatedAt:    time.Now().UTC(),
			}, nil)
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"terminal_id": t.ID,
				"store_id":    t.StoreID,
			}
			if evt, err := outbox.NewEvent(t.TenantID, "device", t.ID, EventDeviceRegistered, 1, payload); err == nil {
				_ = s.outboxWriter.Write(txCtx, evt)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return &TerminalRegistrationResult{
		Terminal:    *term,
		DeviceToken: plainToken,
	}, nil
}

// RevokeDeviceToken invalidates a terminal's device token and disables the terminal.
func (s *DomainService) RevokeDeviceToken(ctx context.Context, actorID *uuid.UUID, terminalID uuid.UUID) error {
	return s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		if _, err := s.store.GetTerminalByID(txCtx, terminalID); err != nil {
			return err
		}

		if err := s.store.UpdateTerminalDeviceHash(txCtx, terminalID, nil, actorID); err != nil {
			return err
		}

		t, err := s.store.GetTerminalByID(txCtx, terminalID)
		if err != nil {
			return err
		}

		t.Status = TerminalStatusDisabled
		t.UpdatedBy = actorID
		if err := s.store.UpdateTerminal(txCtx, t); err != nil {
			return err
		}

		if s.auditStore != nil {
			devResType := "device"
			_ = s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     t.TenantID,
				UserID:       actorID,
				Module:       "organization",
				Action:       AuditActionDeviceRevoked,
				ResourceType: &devResType,
				ResourceID:   &t.ID,
				Status:       audit.StatusSuccess,
				CreatedAt:    time.Now().UTC(),
			}, nil)
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"terminal_id": t.ID,
				"store_id":    t.StoreID,
			}
			if evt, err := outbox.NewEvent(t.TenantID, "device", t.ID, EventDeviceRevoked, 1, payload); err == nil {
				_ = s.outboxWriter.Write(txCtx, evt)
			}
		}

		return nil
	})
}

// VerifyDeviceToken verifies that a provided device token matches the stored hash for an active terminal.
func (s *DomainService) VerifyDeviceToken(ctx context.Context, terminalID uuid.UUID, token string) (*Terminal, error) {
	var term *Terminal
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		t, err := s.store.GetTerminalByID(txCtx, terminalID)
		if err != nil {
			return err
		}

		if t.DeviceIdentifier == nil || *t.DeviceIdentifier == "" {
			return ErrDeviceTokenRevoked
		}
		if t.Status == TerminalStatusRetired {
			return ErrTerminalRetired
		}
		if t.Status != TerminalStatusActive {
			return ErrTerminalDisabled
		}

		if !VerifyDeviceToken(token, *t.DeviceIdentifier) {
			return ErrInvalidDeviceToken
		}

		term = t
		return nil
	})

	if err != nil {
		return nil, err
	}
	return term, nil
}

// ValidateScopeEntity verifies that an entity exists, belongs to the caller's tenant, and is active.
func (s *DomainService) ValidateScopeEntity(ctx context.Context, scopeType string, entityID uuid.UUID) (bool, error) {
	return s.store.ValidateScopeEntity(ctx, scopeType, entityID)
}
