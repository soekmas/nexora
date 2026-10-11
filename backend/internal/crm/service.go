// Package crm provides domain services and business invariants for CRM.
package crm

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

// Service defines domain use-cases for the CRM domain.
type Service interface {
	// Customer Group
	CreateCustomerGroup(ctx context.Context, actorID *uuid.UUID, req CreateCustomerGroupRequest) (*CustomerGroup, error)
	GetCustomerGroup(ctx context.Context, id uuid.UUID) (*CustomerGroup, error)
	UpdateCustomerGroup(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req UpdateCustomerGroupRequest) (*CustomerGroup, error)
	ListCustomerGroups(ctx context.Context, status string, limit, offset int) ([]CustomerGroup, int64, error)

	// Customer
	CreateCustomer(ctx context.Context, actorID *uuid.UUID, req CreateCustomerRequest) (*Customer, error)
	GetCustomer(ctx context.Context, id uuid.UUID) (*Customer, error)
	UpdateCustomer(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req UpdateCustomerRequest) (*Customer, error)
	DisableCustomer(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, expectedVersion *int64) (*Customer, error)
	EnableCustomer(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, expectedVersion *int64) (*Customer, error)
	ListCustomers(ctx context.Context, filter ListCustomersFilter) ([]Customer, int64, error)

	// Customer Address
	CreateAddress(ctx context.Context, actorID *uuid.UUID, customerID uuid.UUID, req CreateAddressRequest) (*CustomerAddress, error)
	GetAddress(ctx context.Context, id uuid.UUID) (*CustomerAddress, error)
	UpdateAddress(ctx context.Context, actorID *uuid.UUID, customerID uuid.UUID, addressID uuid.UUID, req UpdateAddressRequest) (*CustomerAddress, error)
	DeleteAddress(ctx context.Context, actorID *uuid.UUID, customerID uuid.UUID, addressID uuid.UUID) error
	ListAddresses(ctx context.Context, customerID uuid.UUID) ([]CustomerAddress, error)
}

// DomainService implements Service with transactional, audit, and outbox guarantees.
type DomainService struct {
	store        Store
	txManager    postgres.TxManager
	auditStore   audit.Store
	outboxWriter outbox.Writer
	logger       *slog.Logger
}

// NewService constructs a new CRM DomainService.
func NewService(
	store Store,
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
// Customer Group
// ============================================================================

// CreateCustomerGroup creates a new customer segment group.
func (s *DomainService) CreateCustomerGroup(ctx context.Context, actorID *uuid.UUID, req CreateCustomerGroupRequest) (*CustomerGroup, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	req.Code = strings.TrimSpace(req.Code)
	if req.Code == "" {
		return nil, ErrCustomerGroupCodeRequired
	}
	if err := ValidateGroupCode(req.Code); err != nil {
		return nil, err
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, ErrCustomerGroupNameRequired
	}

	g := &CustomerGroup{
		ID:        uuid.New(),
		TenantID:  pc.TenantID,
		Code:      req.Code,
		Name:      req.Name,
		Status:    StatusActive,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		if err := s.store.CreateCustomerGroup(txCtx, g); err != nil {
			return err
		}

		if s.auditStore != nil {
			meta, _ := json.Marshal(map[string]any{
				"code":   g.Code,
				"name":   g.Name,
				"status": g.Status,
			})
			resType := "customer_group"
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     g.TenantID,
				UserID:       actorID,
				Module:       "CRM",
				Action:       AuditActionCustomerGroupCreated,
				ResourceType: &resType,
				ResourceID:   &g.ID,
				Status:       audit.StatusSuccess,
				Metadata:     meta,
				CreatedAt:    time.Now().UTC(),
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"id":     g.ID,
				"code":   g.Code,
				"name":   g.Name,
				"status": g.Status,
			}
			if evt, err := outbox.NewEvent(g.TenantID, "customer_group", g.ID, EventCustomerGroupCreated, 1, payload); err == nil {
				if err := s.outboxWriter.Write(txCtx, evt); err != nil {
					return fmt.Errorf("write outbox: %w", err)
				}
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return g, nil
}

// GetCustomerGroup retrieves a customer group by ID.
func (s *DomainService) GetCustomerGroup(ctx context.Context, id uuid.UUID) (*CustomerGroup, error) {
	var g *CustomerGroup
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		g, err = s.store.GetCustomerGroupByID(txCtx, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return g, nil
}

// UpdateCustomerGroup updates customer group attributes.
func (s *DomainService) UpdateCustomerGroup(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req UpdateCustomerGroupRequest) (*CustomerGroup, error) {
	var updated *CustomerGroup
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		g, err := s.store.GetCustomerGroupByID(txCtx, id)
		if err != nil {
			return err
		}

		if req.Code != nil {
			code := strings.TrimSpace(*req.Code)
			if code == "" {
				return ErrCustomerGroupCodeRequired
			}
			if err := ValidateGroupCode(code); err != nil {
				return err
			}
			g.Code = code
		}
		if req.Name != nil {
			name := strings.TrimSpace(*req.Name)
			if name == "" {
				return ErrCustomerGroupNameRequired
			}
			g.Name = name
		}
		if req.Status != nil {
			st := strings.ToUpper(strings.TrimSpace(*req.Status))
			if st != StatusActive && st != StatusInactive {
				return fmt.Errorf("%w: invalid customer group status %q", ErrInvalidInput, st)
			}
			g.Status = st
		}

		if err := s.store.UpdateCustomerGroup(txCtx, g); err != nil {
			return err
		}

		if s.auditStore != nil {
			meta, _ := json.Marshal(map[string]any{
				"code":   g.Code,
				"name":   g.Name,
				"status": g.Status,
			})
			resType := "customer_group"
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     g.TenantID,
				UserID:       actorID,
				Module:       "CRM",
				Action:       AuditActionCustomerGroupUpdated,
				ResourceType: &resType,
				ResourceID:   &g.ID,
				Status:       audit.StatusSuccess,
				Metadata:     meta,
				CreatedAt:    time.Now().UTC(),
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"id":     g.ID,
				"code":   g.Code,
				"name":   g.Name,
				"status": g.Status,
			}
			if evt, err := outbox.NewEvent(g.TenantID, "customer_group", g.ID, EventCustomerGroupUpdated, 1, payload); err == nil {
				if err := s.outboxWriter.Write(txCtx, evt); err != nil {
					return fmt.Errorf("write outbox: %w", err)
				}
			}
		}

		updated = g
		return nil
	})

	if err != nil {
		return nil, err
	}
	return updated, nil
}

// ListCustomerGroups retrieves customer groups.
func (s *DomainService) ListCustomerGroups(ctx context.Context, status string, limit, offset int) ([]CustomerGroup, int64, error) {
	var groups []CustomerGroup
	var total int64
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		groups, total, err = s.store.ListCustomerGroups(txCtx, status, limit, offset)
		return err
	})
	return groups, total, err
}

// ============================================================================
// Customer
// ============================================================================

// CreateCustomer creates a new customer profile.
func (s *DomainService) CreateCustomer(ctx context.Context, actorID *uuid.UUID, req CreateCustomerRequest) (*Customer, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, ErrCustomerNameRequired
	}

	var normEmail *string
	if req.Email != nil && strings.TrimSpace(*req.Email) != "" {
		email, err := ValidateEmail(*req.Email)
		if err != nil {
			return nil, err
		}
		normEmail = &email
	}

	var normPhone *string
	if req.Phone != nil && strings.TrimSpace(*req.Phone) != "" {
		phone, err := ValidatePhone(*req.Phone)
		if err != nil {
			return nil, err
		}
		normPhone = &phone
	}

	isAutoGenerated := false
	if strings.TrimSpace(req.CustomerNo) == "" {
		isAutoGenerated = true
	} else {
		if err := ValidateCustomerNumber(req.CustomerNo); err != nil {
			return nil, err
		}
	}

	var created *Customer
	maxAttempts := 1
	if isAutoGenerated {
		maxAttempts = 3 // DEC-B1-016: Bounded retry for auto-generated number collisions
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		custNo := req.CustomerNo
		if isAutoGenerated {
			var genErr error
			custNo, genErr = GenerateCustomerNumber(time.Now().UTC())
			if genErr != nil {
				return nil, fmt.Errorf("generate customer number: %w", genErr)
			}
		}

		c := &Customer{
			ID:              uuid.New(),
			TenantID:        pc.TenantID,
			CustomerNo:      custNo,
			CustomerGroupID: req.GroupID,
			Name:            req.Name,
			Email:           normEmail,
			Phone:           normPhone,
			TaxID:           req.TaxID,
			Status:          StatusActive,
			CreatedAt:       time.Now().UTC(),
			UpdatedAt:       time.Now().UTC(),
			CreatedBy:       actorID,
			UpdatedBy:       actorID,
			Version:         1,
		}

		err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
			// Validate group if specified
			if c.CustomerGroupID != nil {
				grp, err := s.store.GetCustomerGroupByID(txCtx, *c.CustomerGroupID)
				if err != nil {
					return err
				}
				if grp.Status != StatusActive {
					return ErrCustomerGroupInactive
				}
			}

			if err := s.store.CreateCustomer(txCtx, c); err != nil {
				return err
			}

			if s.auditStore != nil {
				metaMap := map[string]any{
					"customer_no": c.CustomerNo,
					"name":        c.Name,
				}
				if c.Email != nil {
					metaMap["email"] = MaskEmail(*c.Email)
				}
				if c.Phone != nil {
					metaMap["phone"] = MaskPhone(*c.Phone)
				}
				if c.TaxID != nil {
					metaMap["tax_id"] = MaskTaxID(*c.TaxID)
				}
				if c.CustomerGroupID != nil {
					metaMap["group_id"] = c.CustomerGroupID.String()
				}
				meta, _ := json.Marshal(metaMap)
				resType := "customer"
				if err := s.auditStore.Record(txCtx, &audit.Log{
					TenantID:     c.TenantID,
					UserID:       actorID,
					Module:       "CRM",
					Action:       AuditActionCustomerCreated,
					ResourceType: &resType,
					ResourceID:   &c.ID,
					Status:       audit.StatusSuccess,
					Metadata:     meta,
					CreatedAt:    time.Now().UTC(),
				}, nil); err != nil {
					return fmt.Errorf("record audit: %w", err)
				}
			}

			if s.outboxWriter != nil {
				payload := map[string]any{
					"id":          c.ID,
					"customer_no": c.CustomerNo,
					"name":        c.Name,
					"email":       c.Email,
					"phone":       c.Phone,
					"group_id":    c.CustomerGroupID,
					"status":      c.Status,
				}
				if evt, err := outbox.NewEvent(c.TenantID, "customer", c.ID, EventCustomerCreated, 1, payload); err == nil {
					if err := s.outboxWriter.Write(txCtx, evt); err != nil {
						return fmt.Errorf("write outbox: %w", err)
					}
				}
			}

			created = c
			return nil
		})

		if err == nil {
			return created, nil
		}

		if errors.Is(err, ErrCustomerNumberConflict) && isAutoGenerated {
			lastErr = err
			s.logger.Warn("Collision on auto-generated customer number, retrying",
				slog.Int("attempt", attempt),
				slog.String("customer_no", custNo),
			)
			continue
		}

		return nil, err
	}

	if lastErr != nil {
		return nil, fmt.Errorf("%w: %v", ErrCustomerNumberExhausted, lastErr)
	}
	return nil, ErrCustomerNumberExhausted
}

// GetCustomer retrieves a customer by ID.
func (s *DomainService) GetCustomer(ctx context.Context, id uuid.UUID) (*Customer, error) {
	var c *Customer
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		c, err = s.store.GetCustomerByID(txCtx, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

// UpdateCustomer updates customer profile details enforcing optimistic locking.
func (s *DomainService) UpdateCustomer(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, req UpdateCustomerRequest) (*Customer, error) {
	var updated *Customer
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		c, err := s.store.LockCustomerForUpdate(txCtx, id)
		if err != nil {
			return err
		}

		if req.ExpectedVersion != nil && c.Version != *req.ExpectedVersion {
			return ErrCustomerVersionConflict
		}

		groupChanged := false
		if req.GroupID != nil {
			if c.CustomerGroupID == nil || *c.CustomerGroupID != *req.GroupID {
				grp, err := s.store.GetCustomerGroupByID(txCtx, *req.GroupID)
				if err != nil {
					return err
				}
				if grp.Status != StatusActive {
					return ErrCustomerGroupInactive
				}
				c.CustomerGroupID = req.GroupID
				groupChanged = true
			}
		}

		if req.Name != nil {
			name := strings.TrimSpace(*req.Name)
			if name == "" {
				return ErrCustomerNameRequired
			}
			c.Name = name
		}

		if req.Email != nil {
			if strings.TrimSpace(*req.Email) == "" {
				c.Email = nil
			} else {
				email, err := ValidateEmail(*req.Email)
				if err != nil {
					return err
				}
				c.Email = &email
			}
		}

		if req.Phone != nil {
			if strings.TrimSpace(*req.Phone) == "" {
				c.Phone = nil
			} else {
				phone, err := ValidatePhone(*req.Phone)
				if err != nil {
					return err
				}
				c.Phone = &phone
			}
		}

		if req.TaxID != nil {
			c.TaxID = req.TaxID
		}

		c.UpdatedBy = actorID
		expectedVersion := c.Version
		if err := s.store.UpdateCustomer(txCtx, c, expectedVersion); err != nil {
			return err
		}

		auditAction := AuditActionCustomerUpdated
		if groupChanged {
			auditAction = AuditActionCustomerGroupChanged
		}

		if s.auditStore != nil {
			metaMap := map[string]any{
				"customer_no": c.CustomerNo,
				"name":        c.Name,
				"version":     c.Version,
			}
			if c.Email != nil {
				metaMap["email"] = MaskEmail(*c.Email)
			}
			if c.Phone != nil {
				metaMap["phone"] = MaskPhone(*c.Phone)
			}
			if c.TaxID != nil {
				metaMap["tax_id"] = MaskTaxID(*c.TaxID)
			}
			if c.CustomerGroupID != nil {
				metaMap["group_id"] = c.CustomerGroupID.String()
			}
			meta, _ := json.Marshal(metaMap)
			resType := "customer"
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     c.TenantID,
				UserID:       actorID,
				Module:       "CRM",
				Action:       auditAction,
				ResourceType: &resType,
				ResourceID:   &c.ID,
				Status:       audit.StatusSuccess,
				Metadata:     meta,
				CreatedAt:    time.Now().UTC(),
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"id":          c.ID,
				"customer_no": c.CustomerNo,
				"name":        c.Name,
				"email":       c.Email,
				"phone":       c.Phone,
				"group_id":    c.CustomerGroupID,
				"status":      c.Status,
				"version":     c.Version,
			}
			if evt, err := outbox.NewEvent(c.TenantID, "customer", c.ID, EventCustomerUpdated, 1, payload); err == nil {
				if err := s.outboxWriter.Write(txCtx, evt); err != nil {
					return fmt.Errorf("write outbox: %w", err)
				}
			}
		}

		updated = c
		return nil
	})

	if err != nil {
		return nil, err
	}
	return updated, nil
}

// DisableCustomer transitions a customer's status to INACTIVE.
func (s *DomainService) DisableCustomer(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, expectedVersion *int64) (*Customer, error) {
	var disabled *Customer
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		c, err := s.store.LockCustomerForUpdate(txCtx, id)
		if err != nil {
			return err
		}

		if expectedVersion != nil && c.Version != *expectedVersion {
			return ErrCustomerVersionConflict
		}

		c.Status = StatusInactive
		c.UpdatedBy = actorID
		exp := c.Version
		if err := s.store.UpdateCustomer(txCtx, c, exp); err != nil {
			return err
		}

		if s.auditStore != nil {
			meta, _ := json.Marshal(map[string]any{
				"customer_no": c.CustomerNo,
				"status":      c.Status,
				"version":     c.Version,
			})
			resType := "customer"
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     c.TenantID,
				UserID:       actorID,
				Module:       "CRM",
				Action:       AuditActionCustomerDisabled,
				ResourceType: &resType,
				ResourceID:   &c.ID,
				Status:       audit.StatusSuccess,
				Metadata:     meta,
				CreatedAt:    time.Now().UTC(),
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"id":          c.ID,
				"customer_no": c.CustomerNo,
				"status":      c.Status,
				"version":     c.Version,
			}
			if evt, err := outbox.NewEvent(c.TenantID, "customer", c.ID, EventCustomerDisabled, 1, payload); err == nil {
				if err := s.outboxWriter.Write(txCtx, evt); err != nil {
					return fmt.Errorf("write outbox: %w", err)
				}
			}
		}

		disabled = c
		return nil
	})

	if err != nil {
		return nil, err
	}
	return disabled, nil
}

// EnableCustomer transitions an INACTIVE customer back to ACTIVE.
func (s *DomainService) EnableCustomer(ctx context.Context, actorID *uuid.UUID, id uuid.UUID, expectedVersion *int64) (*Customer, error) {
	var enabled *Customer
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		c, err := s.store.LockCustomerForUpdate(txCtx, id)
		if err != nil {
			return err
		}

		if expectedVersion != nil && c.Version != *expectedVersion {
			return ErrCustomerVersionConflict
		}

		c.Status = StatusActive
		c.UpdatedBy = actorID
		exp := c.Version
		if err := s.store.UpdateCustomer(txCtx, c, exp); err != nil {
			return err
		}

		if s.auditStore != nil {
			meta, _ := json.Marshal(map[string]any{
				"customer_no": c.CustomerNo,
				"status":      c.Status,
				"version":     c.Version,
			})
			resType := "customer"
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     c.TenantID,
				UserID:       actorID,
				Module:       "CRM",
				Action:       AuditActionCustomerUpdated,
				ResourceType: &resType,
				ResourceID:   &c.ID,
				Status:       audit.StatusSuccess,
				Metadata:     meta,
				CreatedAt:    time.Now().UTC(),
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"id":          c.ID,
				"customer_no": c.CustomerNo,
				"status":      c.Status,
				"version":     c.Version,
			}
			if evt, err := outbox.NewEvent(c.TenantID, "customer", c.ID, EventCustomerUpdated, 1, payload); err == nil {
				if err := s.outboxWriter.Write(txCtx, evt); err != nil {
					return fmt.Errorf("write outbox: %w", err)
				}
			}
		}

		enabled = c
		return nil
	})

	if err != nil {
		return nil, err
	}
	return enabled, nil
}

// ListCustomers retrieves a paginated and filtered list of customers.
func (s *DomainService) ListCustomers(ctx context.Context, filter ListCustomersFilter) ([]Customer, int64, error) {
	var customers []Customer
	var total int64
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		customers, total, err = s.store.ListCustomers(txCtx, filter)
		return err
	})
	return customers, total, err
}

// ============================================================================
// Customer Address
// ============================================================================

// CreateAddress adds a new address to a customer's address book.
func (s *DomainService) CreateAddress(ctx context.Context, actorID *uuid.UUID, customerID uuid.UUID, req CreateAddressRequest) (*CustomerAddress, error) {
	pc, ok := postgres.FromContext(ctx)
	if !ok || pc.TenantID == uuid.Nil {
		return nil, postgres.ErrMissingTenantContext
	}

	addrType := strings.ToUpper(strings.TrimSpace(req.AddressType))
	if addrType == "" {
		addrType = strings.ToUpper(strings.TrimSpace(req.Label))
	}
	if addrType == "" {
		addrType = AddressTypeShipping
	}
	if addrType != AddressTypeShipping && addrType != AddressTypeBilling && addrType != AddressTypeOther {
		return nil, ErrInvalidAddressType
	}

	addrLine := strings.TrimSpace(req.AddressLine)
	if addrLine == "" {
		addrLine = strings.TrimSpace(req.AddressLine1)
	}
	if addrLine == "" {
		return nil, ErrAddressLineRequired
	}

	countryCode := strings.ToUpper(strings.TrimSpace(req.CountryCode))
	if countryCode == "" {
		countryCode = "ID"
	}

	var created *CustomerAddress
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		// 1. Lock and verify parent customer
		c, err := s.store.LockCustomerForUpdate(txCtx, customerID)
		if err != nil {
			return err
		}
		if c.Status != StatusActive {
			return ErrCustomerInactive
		}

		// 2. Atomic default address management (DEC-B1-017)
		if req.IsDefault {
			if err := s.store.ClearDefaultAddresses(txCtx, customerID, nil); err != nil {
				return err
			}
		}

		a := &CustomerAddress{
			ID:            uuid.New(),
			TenantID:      pc.TenantID,
			CustomerID:    customerID,
			AddressType:   addrType,
			RecipientName: req.RecipientName,
			Phone:         req.Phone,
			AddressLine:   addrLine,
			City:          req.City,
			Province:      req.Province,
			PostalCode:    req.PostalCode,
			CountryCode:   countryCode,
			IsDefault:     req.IsDefault,
		}

		if err := s.store.CreateCustomerAddress(txCtx, a); err != nil {
			return err
		}

		if s.auditStore != nil {
			metaMap := map[string]any{
				"customer_id":  a.CustomerID.String(),
				"address_type": a.AddressType,
				"is_default":   a.IsDefault,
			}
			if a.Phone != nil {
				metaMap["phone"] = MaskPhone(*a.Phone)
			}
			meta, _ := json.Marshal(metaMap)
			resType := "customer_address"
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     a.TenantID,
				UserID:       actorID,
				Module:       "CRM",
				Action:       AuditActionAddressCreated,
				ResourceType: &resType,
				ResourceID:   &a.ID,
				Status:       audit.StatusSuccess,
				Metadata:     meta,
				CreatedAt:    time.Now().UTC(),
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"id":           a.ID,
				"customer_id":  a.CustomerID,
				"address_type": a.AddressType,
				"city":         a.City,
				"postal_code":  a.PostalCode,
				"is_default":   a.IsDefault,
			}
			if evt, err := outbox.NewEvent(a.TenantID, "customer_address", a.ID, EventAddressCreated, 1, payload); err == nil {
				if err := s.outboxWriter.Write(txCtx, evt); err != nil {
					return fmt.Errorf("write outbox: %w", err)
				}
			}
		}

		created = a
		return nil
	})

	if err != nil {
		return nil, err
	}
	return created, nil
}

// GetAddress retrieves a single address by ID.
func (s *DomainService) GetAddress(ctx context.Context, id uuid.UUID) (*CustomerAddress, error) {
	var a *CustomerAddress
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		var err error
		a, err = s.store.GetCustomerAddressByID(txCtx, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return a, nil
}

// UpdateAddress updates an existing customer address.
func (s *DomainService) UpdateAddress(ctx context.Context, actorID *uuid.UUID, customerID uuid.UUID, addressID uuid.UUID, req UpdateAddressRequest) (*CustomerAddress, error) {
	var updated *CustomerAddress
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		c, err := s.store.LockCustomerForUpdate(txCtx, customerID)
		if err != nil {
			return err
		}
		if c.Status != StatusActive {
			return ErrCustomerInactive
		}

		a, err := s.store.GetCustomerAddressByID(txCtx, addressID)
		if err != nil {
			return err
		}
		if a.CustomerID != customerID {
			return ErrAddressNotFound
		}

		if req.AddressType != nil {
			at := strings.ToUpper(strings.TrimSpace(*req.AddressType))
			if at != AddressTypeShipping && at != AddressTypeBilling && at != AddressTypeOther {
				return ErrInvalidAddressType
			}
			a.AddressType = at
		} else if req.Label != nil {
			at := strings.ToUpper(strings.TrimSpace(*req.Label))
			if at != AddressTypeShipping && at != AddressTypeBilling && at != AddressTypeOther {
				return ErrInvalidAddressType
			}
			a.AddressType = at
		}

		if req.AddressLine != nil {
			al := strings.TrimSpace(*req.AddressLine)
			if al == "" {
				return ErrAddressLineRequired
			}
			a.AddressLine = al
		} else if req.AddressLine1 != nil {
			al := strings.TrimSpace(*req.AddressLine1)
			if al == "" {
				return ErrAddressLineRequired
			}
			a.AddressLine = al
		}

		if req.RecipientName != nil {
			a.RecipientName = req.RecipientName
		}
		if req.Phone != nil {
			a.Phone = req.Phone
		}
		if req.City != nil {
			a.City = req.City
		}
		if req.Province != nil {
			a.Province = req.Province
		}
		if req.PostalCode != nil {
			a.PostalCode = req.PostalCode
		}
		if req.CountryCode != nil {
			a.CountryCode = strings.ToUpper(strings.TrimSpace(*req.CountryCode))
		}

		if req.IsDefault != nil {
			if *req.IsDefault && !a.IsDefault {
				// Clear any other default address atomically
				if err := s.store.ClearDefaultAddresses(txCtx, customerID, &addressID); err != nil {
					return err
				}
			}
			a.IsDefault = *req.IsDefault
		}

		if err := s.store.UpdateCustomerAddress(txCtx, a); err != nil {
			return err
		}

		if s.auditStore != nil {
			metaMap := map[string]any{
				"customer_id":  a.CustomerID.String(),
				"address_type": a.AddressType,
				"is_default":   a.IsDefault,
			}
			if a.Phone != nil {
				metaMap["phone"] = MaskPhone(*a.Phone)
			}
			meta, _ := json.Marshal(metaMap)
			resType := "customer_address"
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     a.TenantID,
				UserID:       actorID,
				Module:       "CRM",
				Action:       AuditActionAddressUpdated,
				ResourceType: &resType,
				ResourceID:   &a.ID,
				Status:       audit.StatusSuccess,
				Metadata:     meta,
				CreatedAt:    time.Now().UTC(),
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"id":           a.ID,
				"customer_id":  a.CustomerID,
				"address_type": a.AddressType,
				"city":         a.City,
				"postal_code":  a.PostalCode,
				"is_default":   a.IsDefault,
			}
			if evt, err := outbox.NewEvent(a.TenantID, "customer_address", a.ID, EventAddressUpdated, 1, payload); err == nil {
				if err := s.outboxWriter.Write(txCtx, evt); err != nil {
					return fmt.Errorf("write outbox: %w", err)
				}
			}
		}

		updated = a
		return nil
	})

	if err != nil {
		return nil, err
	}
	return updated, nil
}

// DeleteAddress deletes an address from a customer's address book.
func (s *DomainService) DeleteAddress(ctx context.Context, actorID *uuid.UUID, customerID uuid.UUID, addressID uuid.UUID) error {
	return s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		c, err := s.store.LockCustomerForUpdate(txCtx, customerID)
		if err != nil {
			return err
		}
		if c.Status != StatusActive {
			return ErrCustomerInactive
		}

		a, err := s.store.GetCustomerAddressByID(txCtx, addressID)
		if err != nil {
			return err
		}
		if a.CustomerID != customerID {
			return ErrAddressNotFound
		}

		if err := s.store.DeleteCustomerAddress(txCtx, addressID); err != nil {
			return err
		}

		if s.auditStore != nil {
			meta, _ := json.Marshal(map[string]any{
				"customer_id":  customerID.String(),
				"address_type": a.AddressType,
			})
			resType := "customer_address"
			if err := s.auditStore.Record(txCtx, &audit.Log{
				TenantID:     a.TenantID,
				UserID:       actorID,
				Module:       "CRM",
				Action:       AuditActionAddressDeleted,
				ResourceType: &resType,
				ResourceID:   &addressID,
				Status:       audit.StatusSuccess,
				Metadata:     meta,
				CreatedAt:    time.Now().UTC(),
			}, nil); err != nil {
				return fmt.Errorf("record audit: %w", err)
			}
		}

		if s.outboxWriter != nil {
			payload := map[string]any{
				"id":          addressID,
				"customer_id": customerID,
			}
			if evt, err := outbox.NewEvent(a.TenantID, "customer_address", addressID, EventAddressDeleted, 1, payload); err == nil {
				if err := s.outboxWriter.Write(txCtx, evt); err != nil {
					return fmt.Errorf("write outbox: %w", err)
				}
			}
		}

		return nil
	})
}

// ListAddresses retrieves all addresses for a customer.
func (s *DomainService) ListAddresses(ctx context.Context, customerID uuid.UUID) ([]CustomerAddress, error) {
	var addresses []CustomerAddress
	err := s.txManager.WithinTx(ctx, func(txCtx context.Context) error {
		// Verify customer exists in tenant
		if _, err := s.store.GetCustomerByID(txCtx, customerID); err != nil {
			return err
		}
		var err error
		addresses, err = s.store.ListAddressesByCustomerID(txCtx, customerID)
		return err
	})
	return addresses, err
}
