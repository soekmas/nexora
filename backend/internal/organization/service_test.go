package organization_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/organization"
	"example.com/nexora/backend/internal/platform/postgres"
)

// mockTxManager executes the callback with the given context (no DB transaction).
type mockTxManager struct{}

func (m *mockTxManager) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func (m *mockTxManager) WithinUnscopedTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func (m *mockTxManager) WithinSavepoint(ctx context.Context, _ string, fn func(context.Context) error) error {
	return fn(ctx)
}

type inMemoryStore struct {
	companies  map[uuid.UUID]*organization.Company
	regions    map[uuid.UUID]*organization.Region
	stores     map[uuid.UUID]*organization.Store
	warehouses map[uuid.UUID]*organization.Warehouse
	terminals  map[uuid.UUID]*organization.Terminal
}

func newInMemoryStore() *inMemoryStore {
	return &inMemoryStore{
		companies:  make(map[uuid.UUID]*organization.Company),
		regions:    make(map[uuid.UUID]*organization.Region),
		stores:     make(map[uuid.UUID]*organization.Store),
		warehouses: make(map[uuid.UUID]*organization.Warehouse),
		terminals:  make(map[uuid.UUID]*organization.Terminal),
	}
}

func (s *inMemoryStore) CreateCompany(_ context.Context, c *organization.Company) error {
	for _, existing := range s.companies {
		if existing.TenantID == c.TenantID && existing.Code == c.Code {
			return organization.ErrCompanyCodeExists
		}
	}
	s.companies[c.ID] = c
	return nil
}
func (s *inMemoryStore) GetCompanyByID(_ context.Context, id uuid.UUID) (*organization.Company, error) {
	c, ok := s.companies[id]
	if !ok {
		return nil, organization.ErrCompanyNotFound
	}
	return c, nil
}
func (s *inMemoryStore) GetCompanyByCode(_ context.Context, code string) (*organization.Company, error) {
	for _, c := range s.companies {
		if c.Code == code {
			return c, nil
		}
	}
	return nil, organization.ErrCompanyNotFound
}
func (s *inMemoryStore) UpdateCompany(_ context.Context, c *organization.Company) error {
	existing, ok := s.companies[c.ID]
	if !ok {
		return organization.ErrCompanyNotFound
	}
	if existing.Version != c.Version {
		return organization.ErrOptimisticLockConflict
	}
	c.Version++
	s.companies[c.ID] = c
	return nil
}
func (s *inMemoryStore) ListCompanies(_ context.Context, _, _ int) ([]organization.Company, int64, error) {
	var list []organization.Company
	for _, c := range s.companies {
		list = append(list, *c)
	}
	return list, int64(len(list)), nil
}

func (s *inMemoryStore) CreateRegion(_ context.Context, r *organization.Region) error {
	for _, existing := range s.regions {
		if existing.CompanyID == r.CompanyID && existing.Code == r.Code {
			return organization.ErrRegionCodeExists
		}
	}
	s.regions[r.ID] = r
	return nil
}
func (s *inMemoryStore) GetRegionByID(_ context.Context, id uuid.UUID) (*organization.Region, error) {
	r, ok := s.regions[id]
	if !ok {
		return nil, organization.ErrRegionNotFound
	}
	return r, nil
}
func (s *inMemoryStore) GetRegionByCode(_ context.Context, companyID uuid.UUID, code string) (*organization.Region, error) {
	for _, r := range s.regions {
		if r.CompanyID == companyID && r.Code == code {
			return r, nil
		}
	}
	return nil, organization.ErrRegionNotFound
}
func (s *inMemoryStore) UpdateRegion(_ context.Context, r *organization.Region) error {
	existing, ok := s.regions[r.ID]
	if !ok {
		return organization.ErrRegionNotFound
	}
	if existing.Version != r.Version {
		return organization.ErrOptimisticLockConflict
	}
	r.Version++
	s.regions[r.ID] = r
	return nil
}
func (s *inMemoryStore) ListRegions(_ context.Context, _ *uuid.UUID, _, _ int) ([]organization.Region, int64, error) {
	var list []organization.Region
	for _, r := range s.regions {
		list = append(list, *r)
	}
	return list, int64(len(list)), nil
}

func (s *inMemoryStore) CreateStore(_ context.Context, st *organization.Store) error {
	for _, existing := range s.stores {
		if existing.CompanyID == st.CompanyID && existing.Code == st.Code {
			return organization.ErrStoreCodeExists
		}
	}
	s.stores[st.ID] = st
	return nil
}
func (s *inMemoryStore) GetStoreByID(_ context.Context, id uuid.UUID) (*organization.Store, error) {
	st, ok := s.stores[id]
	if !ok {
		return nil, organization.ErrStoreNotFound
	}
	return st, nil
}
func (s *inMemoryStore) GetStoreByCode(_ context.Context, companyID uuid.UUID, code string) (*organization.Store, error) {
	for _, st := range s.stores {
		if st.CompanyID == companyID && st.Code == code {
			return st, nil
		}
	}
	return nil, organization.ErrStoreNotFound
}
func (s *inMemoryStore) UpdateStore(_ context.Context, st *organization.Store) error {
	existing, ok := s.stores[st.ID]
	if !ok {
		return organization.ErrStoreNotFound
	}
	if existing.Version != st.Version {
		return organization.ErrOptimisticLockConflict
	}
	st.Version++
	s.stores[st.ID] = st
	return nil
}
func (s *inMemoryStore) ListStores(_ context.Context, _ *uuid.UUID, _, _ int) ([]organization.Store, int64, error) {
	var list []organization.Store
	for _, st := range s.stores {
		list = append(list, *st)
	}
	return list, int64(len(list)), nil
}

func (s *inMemoryStore) CreateWarehouse(_ context.Context, w *organization.Warehouse) error {
	for _, existing := range s.warehouses {
		if existing.CompanyID == w.CompanyID && existing.Code == w.Code {
			return organization.ErrWarehouseCodeExists
		}
	}
	s.warehouses[w.ID] = w
	return nil
}
func (s *inMemoryStore) GetWarehouseByID(_ context.Context, id uuid.UUID) (*organization.Warehouse, error) {
	w, ok := s.warehouses[id]
	if !ok {
		return nil, organization.ErrWarehouseNotFound
	}
	return w, nil
}
func (s *inMemoryStore) GetWarehouseByCode(_ context.Context, companyID uuid.UUID, code string) (*organization.Warehouse, error) {
	for _, w := range s.warehouses {
		if w.CompanyID == companyID && w.Code == code {
			return w, nil
		}
	}
	return nil, organization.ErrWarehouseNotFound
}
func (s *inMemoryStore) UpdateWarehouse(_ context.Context, w *organization.Warehouse) error {
	existing, ok := s.warehouses[w.ID]
	if !ok {
		return organization.ErrWarehouseNotFound
	}
	if existing.Version != w.Version {
		return organization.ErrOptimisticLockConflict
	}
	w.Version++
	s.warehouses[w.ID] = w
	return nil
}
func (s *inMemoryStore) ListWarehouses(_ context.Context, _ *uuid.UUID, _ *uuid.UUID, _, _ int) ([]organization.Warehouse, int64, error) {
	var list []organization.Warehouse
	for _, w := range s.warehouses {
		list = append(list, *w)
	}
	return list, int64(len(list)), nil
}

func (s *inMemoryStore) CreateTerminal(_ context.Context, t *organization.Terminal) error {
	for _, existing := range s.terminals {
		if existing.StoreID == t.StoreID && existing.Code == t.Code {
			return organization.ErrTerminalCodeExists
		}
	}
	s.terminals[t.ID] = t
	return nil
}
func (s *inMemoryStore) GetTerminalByID(_ context.Context, id uuid.UUID) (*organization.Terminal, error) {
	t, ok := s.terminals[id]
	if !ok {
		return nil, organization.ErrTerminalNotFound
	}
	return t, nil
}
func (s *inMemoryStore) GetTerminalByCode(_ context.Context, storeID uuid.UUID, code string) (*organization.Terminal, error) {
	for _, t := range s.terminals {
		if t.StoreID == storeID && t.Code == code {
			return t, nil
		}
	}
	return nil, organization.ErrTerminalNotFound
}
func (s *inMemoryStore) GetTerminalByDeviceHash(_ context.Context, deviceHash string) (*organization.Terminal, error) {
	for _, t := range s.terminals {
		if t.DeviceIdentifier != nil && *t.DeviceIdentifier == deviceHash {
			return t, nil
		}
	}
	return nil, organization.ErrTerminalNotFound
}
func (s *inMemoryStore) UpdateTerminal(_ context.Context, t *organization.Terminal) error {
	existing, ok := s.terminals[t.ID]
	if !ok {
		return organization.ErrTerminalNotFound
	}
	if existing.Version != t.Version {
		return organization.ErrOptimisticLockConflict
	}
	t.Version++
	s.terminals[t.ID] = t
	return nil
}
func (s *inMemoryStore) UpdateTerminalDeviceHash(_ context.Context, terminalID uuid.UUID, deviceHash *string, _ *uuid.UUID) error {
	t, ok := s.terminals[terminalID]
	if !ok {
		return organization.ErrTerminalNotFound
	}
	t.DeviceIdentifier = deviceHash
	t.Version++
	return nil
}
func (s *inMemoryStore) ListTerminals(_ context.Context, _ *uuid.UUID, _, _ int) ([]organization.Terminal, int64, error) {
	var list []organization.Terminal
	for _, t := range s.terminals {
		list = append(list, *t)
	}
	return list, int64(len(list)), nil
}

func (s *inMemoryStore) ValidateScopeEntity(_ context.Context, scopeType string, entityID uuid.UUID) (bool, error) {
	switch scopeType {
	case "COMPANY":
		c, ok := s.companies[entityID]
		return ok && c.Status == organization.StatusActive, nil
	case "STORE":
		st, ok := s.stores[entityID]
		return ok && st.Status == organization.StatusActive, nil
	case "WAREHOUSE":
		w, ok := s.warehouses[entityID]
		return ok && w.Status == organization.StatusActive, nil
	case "TERMINAL":
		t, ok := s.terminals[entityID]
		return ok && t.Status == organization.TerminalStatusActive, nil
	}
	return false, nil
}

func TestService_ParentChildValidation(t *testing.T) {
	store := newInMemoryStore()
	tm := &mockTxManager{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := organization.NewService(store, tm, nil, nil, logger)

	tenantA := uuid.New()
	tenantB := uuid.New()
	actor := uuid.New()

	ctxA := postgres.WithTenantID(context.Background(), tenantA)

	// Create Company in Tenant A
	compA, err := svc.CreateCompany(ctxA, &actor, organization.CreateCompanyRequest{
		Code: "CORP-A",
		Name: "Corporation A",
	})
	if err != nil {
		t.Fatalf("failed to create company: %v", err)
	}

	// Create Region in Tenant B referencing Company in Tenant A -> fails
	ctxB := postgres.WithTenantID(context.Background(), tenantB)
	_, err = svc.CreateRegion(ctxB, &actor, organization.CreateRegionRequest{
		CompanyID: compA.ID,
		Code:      "REG-B",
		Name:      "Region B",
	})
	if !errors.Is(err, organization.ErrParentTenantMismatch) {
		t.Fatalf("expected ErrParentTenantMismatch, got %v", err)
	}

	// Create Store with nonexistent region -> fails
	fakeRegionID := uuid.New()
	_, err = svc.CreateStore(ctxA, &actor, organization.CreateStoreRequest{
		CompanyID: compA.ID,
		RegionID:  &fakeRegionID,
		Code:      "STORE-1",
		Name:      "Store 1",
	})
	if !errors.Is(err, organization.ErrParentNotFound) {
		t.Fatalf("expected ErrParentNotFound, got %v", err)
	}
}

func TestService_DeviceTokenLifecycle(t *testing.T) {
	store := newInMemoryStore()
	tm := &mockTxManager{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := organization.NewService(store, tm, nil, nil, logger)

	tenantID := uuid.New()
	actor := uuid.New()
	ctx := postgres.WithTenantID(context.Background(), tenantID)

	// 1. Setup Company and Store
	comp, _ := svc.CreateCompany(ctx, &actor, organization.CreateCompanyRequest{Code: "COMP", Name: "Comp"})
	st, _ := svc.CreateStore(ctx, &actor, organization.CreateStoreRequest{CompanyID: comp.ID, Code: "STR1", Name: "Store 1"})

	// 2. Register Terminal
	res, err := svc.RegisterTerminal(ctx, &actor, organization.RegisterTerminalRequest{
		CompanyID: comp.ID,
		StoreID:   st.ID,
		Code:      "TERM1",
		Name:      "POS Terminal 1",
	})
	if err != nil {
		t.Fatalf("failed to register terminal: %v", err)
	}

	if res.DeviceToken == "" {
		t.Fatalf("expected plaintext token returned on registration")
	}

	// 3. Verify Genuine Token
	term, err := svc.VerifyDeviceToken(ctx, res.Terminal.ID, res.DeviceToken)
	if err != nil {
		t.Fatalf("verify genuine token failed: %v", err)
	}
	if term.ID != res.Terminal.ID {
		t.Fatalf("mismatched terminal ID")
	}

	// 4. Verify Wrong Token
	_, err = svc.VerifyDeviceToken(ctx, res.Terminal.ID, "dev_tok_wrongtoken")
	if !errors.Is(err, organization.ErrInvalidDeviceToken) {
		t.Fatalf("expected ErrInvalidDeviceToken, got %v", err)
	}

	// 5. Rotate Token
	rotRes, err := svc.RotateDeviceToken(ctx, &actor, res.Terminal.ID)
	if err != nil {
		t.Fatalf("rotate device token failed: %v", err)
	}

	// Old token should now fail
	_, err = svc.VerifyDeviceToken(ctx, res.Terminal.ID, res.DeviceToken)
	if !errors.Is(err, organization.ErrInvalidDeviceToken) {
		t.Fatalf("expected old token to fail after rotation, got %v", err)
	}

	// New token should succeed
	_, err = svc.VerifyDeviceToken(ctx, res.Terminal.ID, rotRes.DeviceToken)
	if err != nil {
		t.Fatalf("new token verification failed: %v", err)
	}

	// 6. Revoke Token
	if err := svc.RevokeDeviceToken(ctx, &actor, res.Terminal.ID); err != nil {
		t.Fatalf("revoke token failed: %v", err)
	}

	// Even new token should now fail with revocation/disabled error
	_, err = svc.VerifyDeviceToken(ctx, res.Terminal.ID, rotRes.DeviceToken)
	if !errors.Is(err, organization.ErrDeviceTokenRevoked) && !errors.Is(err, organization.ErrTerminalDisabled) {
		t.Fatalf("expected revoked/disabled error, got %v", err)
	}
}
