package pricing_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/platform/audit"
	"example.com/nexora/backend/internal/platform/auth"
	"example.com/nexora/backend/internal/platform/outbox"
	"example.com/nexora/backend/internal/platform/postgres"
	"example.com/nexora/backend/internal/pricing"
)

// mockStore implements pricing.Store in-memory for service unit tests.
type mockStore struct {
	priceLists     map[uuid.UUID]*pricing.PriceList
	priceRules     map[uuid.UUID]*pricing.PriceRule
	stores         map[uuid.UUID]storeData
	customerGroups map[uuid.UUID]*uuid.UUID
	variants       map[uuid.UUID]uuid.UUID // variantID -> productID
}

type storeData struct {
	companyID uuid.UUID
	status    string
}

func newMockStore() *mockStore {
	return &mockStore{
		priceLists:     make(map[uuid.UUID]*pricing.PriceList),
		priceRules:     make(map[uuid.UUID]*pricing.PriceRule),
		stores:         make(map[uuid.UUID]storeData),
		customerGroups: make(map[uuid.UUID]*uuid.UUID),
		variants:       make(map[uuid.UUID]uuid.UUID),
	}
}

func (m *mockStore) CreatePriceList(ctx context.Context, pl *pricing.PriceList) error {
	m.priceLists[pl.ID] = pl
	return nil
}

func (m *mockStore) GetPriceListByID(ctx context.Context, id uuid.UUID) (*pricing.PriceList, error) {
	pl, ok := m.priceLists[id]
	if !ok {
		return nil, pricing.ErrPriceListNotFound404
	}
	return pl, nil
}

func (m *mockStore) GetPriceListByCode(ctx context.Context, code string) (*pricing.PriceList, error) {
	for _, pl := range m.priceLists {
		if pl.Code == code {
			return pl, nil
		}
	}
	return nil, pricing.ErrPriceListNotFound404
}

func (m *mockStore) UpdatePriceList(ctx context.Context, pl *pricing.PriceList, expectedVersion int64) error {
	existing, ok := m.priceLists[pl.ID]
	if !ok {
		return pricing.ErrPriceListNotFound404
	}
	if existing.Version != expectedVersion {
		return pricing.ErrVersionConflict
	}
	pl.Version = expectedVersion + 1
	m.priceLists[pl.ID] = pl
	return nil
}

func (m *mockStore) ListPriceLists(ctx context.Context, filter pricing.PriceListFilter) ([]*pricing.PriceList, int, error) {
	var res []*pricing.PriceList
	for _, pl := range m.priceLists {
		res = append(res, pl)
	}
	return res, len(res), nil
}

func (m *mockStore) GetDefaultPriceList(ctx context.Context, currency, channel string) (*pricing.PriceList, error) {
	for _, pl := range m.priceLists {
		if pl.IsDefault && pl.Status == pricing.PriceListStatusActive && pl.Currency == currency && pl.Channel == channel && pl.CompanyID == nil {
			return pl, nil
		}
	}
	return nil, pricing.ErrPriceListNotFound
}

func (m *mockStore) CreatePriceRule(ctx context.Context, pr *pricing.PriceRule) error {
	m.priceRules[pr.ID] = pr
	return nil
}

func (m *mockStore) GetPriceRuleByID(ctx context.Context, id uuid.UUID) (*pricing.PriceRule, error) {
	pr, ok := m.priceRules[id]
	if !ok {
		return nil, pricing.ErrPriceRuleNotFound404
	}
	return pr, nil
}

func (m *mockStore) GetPriceRuleByIDForUpdate(ctx context.Context, id uuid.UUID) (*pricing.PriceRule, error) {
	return m.GetPriceRuleByID(ctx, id)
}

func (m *mockStore) UpdatePriceRule(ctx context.Context, pr *pricing.PriceRule, expectedVersion int64) error {
	existing, ok := m.priceRules[pr.ID]
	if !ok {
		return pricing.ErrPriceRuleNotFound404
	}
	if existing.Version != expectedVersion {
		return pricing.ErrVersionConflict
	}
	pr.Version = expectedVersion + 1
	m.priceRules[pr.ID] = pr
	return nil
}

func (m *mockStore) ListPriceRules(ctx context.Context, filter pricing.PriceRuleFilter) ([]*pricing.PriceRule, int, error) {
	var res []*pricing.PriceRule
	for _, pr := range m.priceRules {
		res = append(res, pr)
	}
	return res, len(res), nil
}

func (m *mockStore) GetCandidateRules(ctx context.Context, priceListID uuid.UUID, productIDs []uuid.UUID, variantIDs []uuid.UUID, refTime time.Time) ([]*pricing.PriceRule, error) {
	var candidates []*pricing.PriceRule
	for _, pr := range m.priceRules {
		if pr.PriceListID != priceListID || pr.Status != pricing.PriceRuleStatusActive {
			continue
		}
		if refTime.Before(pr.EffectiveFrom) || (pr.EffectiveTo != nil && !refTime.Before(*pr.EffectiveTo)) {
			continue
		}
		match := false
		for _, pID := range productIDs {
			if pr.ProductID == pID {
				match = true
				break
			}
		}
		if !match && pr.VariantID != nil {
			for _, vID := range variantIDs {
				if *pr.VariantID == vID {
					match = true
					break
				}
			}
		}
		if match {
			candidates = append(candidates, pr)
		}
	}
	return candidates, nil
}

func (m *mockStore) GetStore(ctx context.Context, storeID uuid.UUID) (companyID uuid.UUID, status string, err error) {
	s, ok := m.stores[storeID]
	if !ok {
		return uuid.Nil, "", pricing.ErrStoreNotFound
	}
	return s.companyID, s.status, nil
}

func (m *mockStore) GetCustomerGroup(ctx context.Context, customerID uuid.UUID) (customerGroupID *uuid.UUID, status string, err error) {
	cg, ok := m.customerGroups[customerID]
	if !ok {
		return nil, "", nil
	}
	return cg, "ACTIVE", nil
}

func (m *mockStore) GetVariants(ctx context.Context, variantIDs []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	res := make(map[uuid.UUID]uuid.UUID)
	for _, vID := range variantIDs {
		if pID, ok := m.variants[vID]; ok {
			res[vID] = pID
		}
	}
	return res, nil
}

// mockTxManager executes callback directly
type mockTxManager struct{}

func (m *mockTxManager) WithinTx(ctx context.Context, fn func(txCtx context.Context) error) error {
	return fn(ctx)
}

func (m *mockTxManager) WithinUnscopedTx(ctx context.Context, fn func(txCtx context.Context) error) error {
	return fn(ctx)
}

func (m *mockTxManager) WithinSavepoint(ctx context.Context, name string, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

// mockAuditStore records audit entries in-memory
type mockAuditStore struct {
	logs []*audit.Log
}

func (m *mockAuditStore) Record(ctx context.Context, entry *audit.Log, changes []*audit.DataChange) error {
	m.logs = append(m.logs, entry)
	return nil
}

func (m *mockAuditStore) RecordStandalone(ctx context.Context, entry *audit.Log) error {
	m.logs = append(m.logs, entry)
	return nil
}

func (m *mockAuditStore) RecordSecurityEvent(ctx context.Context, evt *audit.SecurityEvent) error {
	return nil
}

func (m *mockAuditStore) RecordLoginHistory(ctx context.Context, entry *audit.LoginHistory) error {
	return nil
}

func (m *mockAuditStore) FindAuditLogs(ctx context.Context, filter audit.Filter) ([]*audit.Log, int, error) {
	return m.logs, len(m.logs), nil
}

func (m *mockAuditStore) GetDataChanges(ctx context.Context, auditLogID int64) ([]*audit.DataChange, error) {
	return nil, nil
}

// mockOutboxWriter records outbox events in-memory
type mockOutboxWriter struct {
	events []*outbox.Event
}

func (m *mockOutboxWriter) Write(ctx context.Context, event *outbox.Event) error {
	m.events = append(m.events, event)
	return nil
}

func (m *mockOutboxWriter) WriteBatch(ctx context.Context, events []*outbox.Event) error {
	m.events = append(m.events, events...)
	return nil
}

func TestPricingService_QuoteCalculations(t *testing.T) {
	tenantID := uuid.New()
	storeID := uuid.New()
	companyID := uuid.New()
	productID := uuid.New()
	variant1ID := uuid.New()
	variant2ID := uuid.New()

	store := newMockStore()
	store.stores[storeID] = storeData{companyID: companyID, status: "ACTIVE"}
	store.variants[variant1ID] = productID
	store.variants[variant2ID] = productID

	// Create default price list
	priceListID := uuid.New()
	store.priceLists[priceListID] = &pricing.PriceList{
		ID:        priceListID,
		TenantID:  tenantID,
		Code:      "DEFAULT-IDR",
		Name:      "Default IDR List",
		Currency:  "IDR",
		Channel:   "POS",
		Status:    pricing.PriceListStatusActive,
		IsDefault: true,
		Version:   1,
	}

	// Add candidate rules
	rule1 := &pricing.PriceRule{
		ID:            uuid.New(),
		TenantID:      tenantID,
		PriceListID:   priceListID,
		ProductID:     productID,
		VariantID:     &variant1ID,
		MinQty:        1,
		UnitPrice:     15000.0,
		Priority:      100,
		EffectiveFrom: time.Now().Add(-24 * time.Hour),
		Status:        pricing.PriceRuleStatusActive,
		Version:       1,
	}
	rule2 := &pricing.PriceRule{
		ID:            uuid.New(),
		TenantID:      tenantID,
		PriceListID:   priceListID,
		ProductID:     productID,
		VariantID:     &variant2ID,
		MinQty:        1,
		UnitPrice:     25000.0,
		Priority:      100,
		EffectiveFrom: time.Now().Add(-24 * time.Hour),
		Status:        pricing.PriceRuleStatusActive,
		Version:       1,
	}
	store.priceRules[rule1.ID] = rule1
	store.priceRules[rule2.ID] = rule2

	auditMock := &mockAuditStore{}
	outboxMock := &mockOutboxWriter{}
	svc := pricing.NewService(store, &mockTxManager{}, auditMock, outboxMock, nil)

	ctx := postgres.WithPrincipalContext(context.Background(), postgres.PrincipalContext{
		TenantID: tenantID,
		UserID:   uuid.New(),
	})

	t.Run("Successful single-item and multi-item quote with base pricing only", func(t *testing.T) {
		req := pricing.PriceQuoteRequest{
			StoreID:  storeID,
			Currency: "IDR",
			Channel:  "POS",
			Items: []pricing.CartItem{
				{VariantID: variant1ID, Quantity: 2},
				{VariantID: variant2ID, Quantity: 3},
			},
		}

		quote, err := svc.CalculateQuote(ctx, req)
		if err != nil {
			t.Fatalf("unexpected quote error: %v", err)
		}

		if len(quote.Items) != 2 {
			t.Fatalf("expected 2 items, got %d", len(quote.Items))
		}
		// Item 1: 15,000 * 2 = 30,000
		if quote.Items[0].UnitPrice != 15000.0 || quote.Items[0].NetAmount != 30000.0 {
			t.Errorf("item 0 pricing mismatch: %+v", quote.Items[0])
		}
		// Item 2: 25,000 * 3 = 75,000
		if quote.Items[1].UnitPrice != 25000.0 || quote.Items[1].NetAmount != 75000.0 {
			t.Errorf("item 1 pricing mismatch: %+v", quote.Items[1])
		}
		// DEC-P2-022: discount and taxes must be zero
		if quote.DiscountTotal != 0.0000 || quote.TaxTotal != 0.0000 {
			t.Errorf("discount/tax total must be zero: discount=%f, tax=%f", quote.DiscountTotal, quote.TaxTotal)
		}
		if quote.Subtotal != 105000.0 || quote.GrandTotal != 105000.0 {
			t.Errorf("expected grand_total 105000, got %f", quote.GrandTotal)
		}
	})

	t.Run("Missing default price list returns ErrPriceListNotFound", func(t *testing.T) {
		req := pricing.PriceQuoteRequest{
			StoreID:  storeID,
			Currency: "USD", // No USD default exists
			Channel:  "POS",
			Items: []pricing.CartItem{
				{VariantID: variant1ID, Quantity: 1},
			},
		}

		_, err := svc.CalculateQuote(ctx, req)
		if !errors.Is(err, pricing.ErrPriceListNotFound) {
			t.Fatalf("expected ErrPriceListNotFound, got %v", err)
		}
	})

	t.Run("Exact channel default is absent but ALL default exists: do not fall back", func(t *testing.T) {
		usdListID := uuid.New()
		store.priceLists[usdListID] = &pricing.PriceList{
			ID:        usdListID,
			TenantID:  tenantID,
			Code:      "DEFAULT-USD-ALL",
			Name:      "USD All Channel Default",
			Currency:  "USD",
			Channel:   "ALL",
			Status:    pricing.PriceListStatusActive,
			IsDefault: true,
			Version:   1,
		}

		req := pricing.PriceQuoteRequest{
			StoreID:  storeID,
			Currency: "USD",
			Channel:  "POS", // exact POS default absent
			Items: []pricing.CartItem{
				{VariantID: variant1ID, Quantity: 1},
			},
		}

		_, err := svc.CalculateQuote(ctx, req)
		if !errors.Is(err, pricing.ErrPriceListNotFound) {
			t.Fatalf("expected ErrPriceListNotFound when channel POS default absent despite ALL existing, got %v", err)
		}
	})

	t.Run("Explicit price list: accept when store company matches, reject on mismatch or inactive", func(t *testing.T) {
		// Compatible list with matching store company
		compatListID := uuid.New()
		store.priceLists[compatListID] = &pricing.PriceList{
			ID:        compatListID,
			TenantID:  tenantID,
			CompanyID: &companyID,
			Code:      "COMPAT-LIST",
			Currency:  "IDR",
			Channel:   "POS",
			Status:    pricing.PriceListStatusActive,
			Version:   1,
		}
		compatRuleID := uuid.New()
		store.priceRules[compatRuleID] = &pricing.PriceRule{
			ID:            compatRuleID,
			TenantID:      tenantID,
			PriceListID:   compatListID,
			ProductID:     productID,
			MinQty:        1,
			UnitPrice:     12000.0,
			Priority:      100,
			EffectiveFrom: time.Now().Add(-1 * time.Hour),
			Status:        pricing.PriceRuleStatusActive,
			Version:       1,
		}

		req := pricing.PriceQuoteRequest{
			StoreID:     storeID,
			PriceListID: &compatListID,
			Currency:    "IDR",
			Channel:     "POS",
			Items: []pricing.CartItem{
				{VariantID: variant1ID, Quantity: 2},
			},
		}
		quote, err := svc.CalculateQuote(ctx, req)
		if err != nil {
			t.Fatalf("calculate quote with compatible explicit list: %v", err)
		}
		if quote.Currency != "IDR" || quote.GrandTotal <= 0 {
			t.Errorf("expected valid quote, got %+v", quote)
		}

		// Company mismatch list
		otherCompanyID := uuid.New()
		mismatchListID := uuid.New()
		store.priceLists[mismatchListID] = &pricing.PriceList{
			ID:        mismatchListID,
			TenantID:  tenantID,
			CompanyID: &otherCompanyID,
			Code:      "MISMATCH-LIST",
			Currency:  "IDR",
			Channel:   "POS",
			Status:    pricing.PriceListStatusActive,
			Version:   1,
		}
		req.PriceListID = &mismatchListID
		_, err = svc.CalculateQuote(ctx, req)
		if !errors.Is(err, pricing.ErrUnauthorizedPriceList) {
			t.Fatalf("expected ErrUnauthorizedPriceList, got %v", err)
		}

		// Inactive explicit list
		inactiveListID := uuid.New()
		store.priceLists[inactiveListID] = &pricing.PriceList{
			ID:        inactiveListID,
			TenantID:  tenantID,
			CompanyID: &companyID,
			Code:      "INACTIVE-LIST",
			Currency:  "IDR",
			Channel:   "POS",
			Status:    pricing.PriceListStatusInactive,
			Version:   1,
		}
		req.PriceListID = &inactiveListID
		_, err = svc.CalculateQuote(ctx, req)
		if !errors.Is(err, pricing.ErrPriceNotFound) {
			t.Fatalf("expected ErrPriceNotFound for inactive explicit price list, got %v", err)
		}
	})

	t.Run("Currency mismatch on explicit price list returns ErrCurrencyMismatch", func(t *testing.T) {
		req := pricing.PriceQuoteRequest{
			StoreID:     storeID,
			PriceListID: &priceListID, // IDR price list
			Currency:    "USD",        // requested USD
			Channel:     "POS",
			Items: []pricing.CartItem{
				{VariantID: variant1ID, Quantity: 1},
			},
		}

		_, err := svc.CalculateQuote(ctx, req)
		if !errors.Is(err, pricing.ErrCurrencyMismatch) {
			t.Fatalf("expected ErrCurrencyMismatch, got %v", err)
		}
	})

	t.Run("Missing price returns MissingPriceError with unpriced items details (all-or-nothing)", func(t *testing.T) {
		unpricedVariantID := uuid.New()
		req := pricing.PriceQuoteRequest{
			StoreID:  storeID,
			Currency: "IDR",
			Channel:  "POS",
			Items: []pricing.CartItem{
				{VariantID: variant1ID, Quantity: 1},         // Priced (index 0)
				{VariantID: unpricedVariantID, Quantity: 2}, // Unpriced (index 1)
			},
		}

		quote, err := svc.CalculateQuote(ctx, req)
		if quote != nil {
			t.Fatalf("expected nil quote on unpriced items")
		}
		var missingErr *pricing.MissingPriceError
		if !errors.As(err, &missingErr) {
			t.Fatalf("expected MissingPriceError, got %v", err)
		}
		if len(missingErr.UnpricedItems) != 1 {
			t.Fatalf("expected 1 unpriced item, got %d", len(missingErr.UnpricedItems))
		}
		if missingErr.UnpricedItems[0].Index != 1 || missingErr.UnpricedItems[0].VariantID != unpricedVariantID.String() {
			t.Errorf("unpriced item mismatch: %+v", missingErr.UnpricedItems[0])
		}
	})
}

func TestPricingService_RuleLifecycleAndAudit(t *testing.T) {
	tenantID := uuid.New()
	userID := uuid.New()
	store := newMockStore()
	auditMock := &mockAuditStore{}
	outboxMock := &mockOutboxWriter{}
	svc := pricing.NewService(store, &mockTxManager{}, auditMock, outboxMock, nil)

	ctx := postgres.WithPrincipalContext(context.Background(), postgres.PrincipalContext{
		TenantID: tenantID,
		UserID:   userID,
	})
	ctx = auth.WithPrincipal(ctx, &auth.AuthenticatedPrincipal{
		UserID:   userID,
		TenantID: tenantID,
	})

	priceListID := uuid.New()
	productID := uuid.New()

	t.Run("Create draft rule emits PRICE_CREATED audit and outbox event", func(t *testing.T) {
		rule, err := svc.CreatePriceRule(ctx, pricing.CreatePriceRuleRequest{
			PriceListID:   priceListID,
			ProductID:     productID,
			MinQty:        1,
			UnitPrice:     100.0,
			Priority:      100,
			EffectiveFrom: time.Now(),
		})
		if err != nil {
			t.Fatalf("create rule: %v", err)
		}
		if rule.Status != pricing.PriceRuleStatusDraft {
			t.Fatalf("expected DRAFT status, got %s", rule.Status)
		}
		if len(auditMock.logs) == 0 || auditMock.logs[len(auditMock.logs)-1].Action != pricing.AuditActionPriceCreated {
			t.Errorf("expected PRICE_CREATED audit log")
		}
		if len(outboxMock.events) == 0 || outboxMock.events[len(outboxMock.events)-1].EventType != pricing.EventPriceCreated {
			t.Errorf("expected EventPriceCreated outbox event")
		}
	})

	t.Run("Submit rule stays in DRAFT and emits PRICE_SUBMITTED", func(t *testing.T) {
		rule, _ := svc.CreatePriceRule(ctx, pricing.CreatePriceRuleRequest{
			PriceListID:   priceListID,
			ProductID:     productID,
			MinQty:        1,
			UnitPrice:     100.0,
			Priority:      100,
			EffectiveFrom: time.Now(),
		})

		submitted, err := svc.SubmitPriceRule(ctx, rule.ID, pricing.RuleTransitionRequest{Reason: "Ready for review"})
		if err != nil {
			t.Fatalf("submit rule: %v", err)
		}
		if submitted.Status != pricing.PriceRuleStatusDraft {
			t.Fatalf("submitted rule must remain in DRAFT, got %s", submitted.Status)
		}
		if auditMock.logs[len(auditMock.logs)-1].Action != pricing.AuditActionPriceSubmitted {
			t.Errorf("expected PRICE_SUBMITTED audit action")
		}
	})

	t.Run("Approve rule transitions DRAFT -> APPROVED and emits event", func(t *testing.T) {
		rule, _ := svc.CreatePriceRule(ctx, pricing.CreatePriceRuleRequest{
			PriceListID:   priceListID,
			ProductID:     productID,
			MinQty:        1,
			UnitPrice:     100.0,
			Priority:      100,
			EffectiveFrom: time.Now(),
		})

		approved, err := svc.ApprovePriceRule(ctx, rule.ID, pricing.RuleTransitionRequest{Version: 1})
		if err != nil {
			t.Fatalf("approve rule: %v", err)
		}
		if approved.Status != pricing.PriceRuleStatusApproved {
			t.Fatalf("expected APPROVED, got %s", approved.Status)
		}
		if auditMock.logs[len(auditMock.logs)-1].Action != pricing.AuditActionPriceApproved {
			t.Errorf("expected PRICE_APPROVED audit action")
		}
		if outboxMock.events[len(outboxMock.events)-1].EventType != pricing.EventPriceApproved {
			t.Errorf("expected EventPriceApproved outbox event")
		}
	})

	t.Run("Activate rule transitions APPROVED -> ACTIVE", func(t *testing.T) {
		rule, _ := svc.CreatePriceRule(ctx, pricing.CreatePriceRuleRequest{
			PriceListID:   priceListID,
			ProductID:     productID,
			MinQty:        1,
			UnitPrice:     100.0,
			Priority:      100,
			EffectiveFrom: time.Now(),
		})
		_, _ = svc.ApprovePriceRule(ctx, rule.ID, pricing.RuleTransitionRequest{Version: 1})

		activated, err := svc.ActivatePriceRule(ctx, rule.ID, pricing.RuleTransitionRequest{Version: 2})
		if err != nil {
			t.Fatalf("activate rule: %v", err)
		}
		if activated.Status != pricing.PriceRuleStatusActive {
			t.Fatalf("expected ACTIVE, got %s", activated.Status)
		}
		if auditMock.logs[len(auditMock.logs)-1].Action != pricing.AuditActionPriceActivated {
			t.Errorf("expected PRICE_ACTIVATED audit action")
		}
		if outboxMock.events[len(outboxMock.events)-1].EventType != pricing.EventPriceActivated {
			t.Errorf("expected EventPriceActivated outbox event")
		}
	})

	t.Run("Deactivate rule transitions ACTIVE -> INACTIVE", func(t *testing.T) {
		rule, _ := svc.CreatePriceRule(ctx, pricing.CreatePriceRuleRequest{
			PriceListID:   priceListID,
			ProductID:     productID,
			MinQty:        1,
			UnitPrice:     100.0,
			Priority:      100,
			EffectiveFrom: time.Now(),
		})
		_, _ = svc.ApprovePriceRule(ctx, rule.ID, pricing.RuleTransitionRequest{Version: 1})
		_, _ = svc.ActivatePriceRule(ctx, rule.ID, pricing.RuleTransitionRequest{Version: 2})

		deactivated, err := svc.DeactivatePriceRule(ctx, rule.ID, pricing.RuleTransitionRequest{Version: 3})
		if err != nil {
			t.Fatalf("deactivate rule: %v", err)
		}
		if deactivated.Status != pricing.PriceRuleStatusInactive {
			t.Fatalf("expected INACTIVE, got %s", deactivated.Status)
		}
		if auditMock.logs[len(auditMock.logs)-1].Action != pricing.AuditActionPriceDeactivated {
			t.Errorf("expected PRICE_DEACTIVATED audit action")
		}
		if outboxMock.events[len(outboxMock.events)-1].EventType != pricing.EventPriceDeactivated {
			t.Errorf("expected EventPriceDeactivated outbox event")
		}
	})

	t.Run("Reject rule stays in DRAFT and emits PRICE_REJECTED", func(t *testing.T) {
		rule, _ := svc.CreatePriceRule(ctx, pricing.CreatePriceRuleRequest{
			PriceListID:   priceListID,
			ProductID:     productID,
			MinQty:        1,
			UnitPrice:     100.0,
			Priority:      100,
			EffectiveFrom: time.Now(),
		})

		rejected, err := svc.RejectPriceRule(ctx, rule.ID, pricing.RuleTransitionRequest{Reason: "Margins too low"})
		if err != nil {
			t.Fatalf("reject rule: %v", err)
		}
		if rejected.Status != pricing.PriceRuleStatusDraft {
			t.Fatalf("rejected rule must remain in DRAFT, got %s", rejected.Status)
		}
		if auditMock.logs[len(auditMock.logs)-1].Action != pricing.AuditActionPriceRejected {
			t.Errorf("expected PRICE_REJECTED audit action")
		}
	})

	t.Run("Invalid state transitions are rejected with ErrInvalidStateTransition", func(t *testing.T) {
		// 1. Activating a DRAFT rule directly is prohibited
		draftRule, _ := svc.CreatePriceRule(ctx, pricing.CreatePriceRuleRequest{
			PriceListID:   priceListID,
			ProductID:     productID,
			MinQty:        1,
			UnitPrice:     100.0,
			Priority:      100,
			EffectiveFrom: time.Now(),
		})
		_, err := svc.ActivatePriceRule(ctx, draftRule.ID, pricing.RuleTransitionRequest{Version: 1})
		if !errors.Is(err, pricing.ErrInvalidStateTransition) {
			t.Errorf("expected ErrInvalidStateTransition activating DRAFT, got %v", err)
		}

		// 2. Activating an INACTIVE rule is prohibited (strictly terminal/retired)
		_, _ = svc.ApprovePriceRule(ctx, draftRule.ID, pricing.RuleTransitionRequest{Version: 1})
		_, _ = svc.ActivatePriceRule(ctx, draftRule.ID, pricing.RuleTransitionRequest{Version: 2})
		_, _ = svc.DeactivatePriceRule(ctx, draftRule.ID, pricing.RuleTransitionRequest{Version: 3})

		_, err = svc.ActivatePriceRule(ctx, draftRule.ID, pricing.RuleTransitionRequest{Version: 4})
		if !errors.Is(err, pricing.ErrInvalidStateTransition) {
			t.Errorf("expected ErrInvalidStateTransition activating INACTIVE, got %v", err)
		}

		// 3. Activating an EXPIRED rule is prohibited
		expiredRule := &pricing.PriceRule{
			ID:          uuid.New(),
			TenantID:    tenantID,
			PriceListID: priceListID,
			ProductID:   productID,
			MinQty:      1,
			UnitPrice:   100.0,
			Status:      pricing.PriceRuleStatusExpired,
			Version:     1,
		}
		store.priceRules[expiredRule.ID] = expiredRule
		_, err = svc.ActivatePriceRule(ctx, expiredRule.ID, pricing.RuleTransitionRequest{Version: 1})
		if !errors.Is(err, pricing.ErrInvalidStateTransition) {
			t.Errorf("expected ErrInvalidStateTransition activating EXPIRED, got %v", err)
		}

		// 4. Approving an ACTIVE rule is prohibited
		activeRule := &pricing.PriceRule{
			ID:          uuid.New(),
			TenantID:    tenantID,
			PriceListID: priceListID,
			ProductID:   productID,
			MinQty:      1,
			UnitPrice:   100.0,
			Status:      pricing.PriceRuleStatusActive,
			Version:     1,
		}
		store.priceRules[activeRule.ID] = activeRule
		_, err = svc.ApprovePriceRule(ctx, activeRule.ID, pricing.RuleTransitionRequest{Version: 1})
		if !errors.Is(err, pricing.ErrInvalidStateTransition) {
			t.Errorf("expected ErrInvalidStateTransition approving ACTIVE, got %v", err)
		}
	})
}
