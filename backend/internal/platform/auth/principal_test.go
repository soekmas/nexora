package auth_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"example.com/nexora/backend/internal/platform/auth"
)

func TestAuthenticatedPrincipal_Permissions(t *testing.T) {
	principal := &auth.AuthenticatedPrincipal{
		UserID:      uuid.New(),
		TenantID:    uuid.New(),
		Roles:       []string{"cashier", "store_manager"},
		Permissions: []string{"pos:order:create", "pos:order:read", "inventory:stock:read"},
	}

	if !principal.HasPermission("pos:order:create") {
		t.Error("expected true for assigned permission pos:order:create")
	}
	if !principal.HasPermission("inventory:stock:read") {
		t.Error("expected true for assigned permission inventory:stock:read")
	}
	if principal.HasPermission("pos:order:void") {
		t.Error("expected false for unassigned permission pos:order:void")
	}
	if !principal.HasAnyPermission("pos:order:void", "pos:order:read") {
		t.Error("expected true for HasAnyPermission when one matches")
	}
	if principal.HasAnyPermission("admin:all", "finance:close") {
		t.Error("expected false for HasAnyPermission when none match")
	}
	if !principal.HasRole("cashier") || !principal.HasRole("store_manager") {
		t.Error("expected true for assigned roles")
	}
	if principal.HasRole("admin") {
		t.Error("expected false for unassigned role")
	}
}

func TestAuthenticatedPrincipal_WildcardPermission(t *testing.T) {
	principal := &auth.AuthenticatedPrincipal{
		UserID:      uuid.New(),
		TenantID:    uuid.New(),
		Roles:       []string{"super_admin"},
		Permissions: []string{"*"},
	}

	if !principal.HasPermission("any:arbitrary:permission") {
		t.Error("wildcard permission must match any requested permission")
	}
}

func TestAuthenticatedPrincipal_Scopes(t *testing.T) {
	storeID := uuid.New()
	warehouseID := uuid.New()
	companyID := uuid.New()
	otherStoreID := uuid.New()

	principal := &auth.AuthenticatedPrincipal{
		UserID:   uuid.New(),
		TenantID: uuid.New(),
		Scopes: []auth.UserScope{
			{
				Type:      "COMPANY",
				CompanyID: &companyID,
			},
			{
				Type:    "STORE",
				StoreID: &storeID,
			},
			{
				Type:        "WAREHOUSE",
				WarehouseID: &warehouseID,
			},
		},
		ActiveStoreID: storeID,
	}

	if !principal.HasScope("STORE", storeID) {
		t.Errorf("expected HasScope true for assigned store %s", storeID)
	}
	if principal.HasScope("STORE", otherStoreID) {
		t.Errorf("expected HasScope false for unassigned store %s", otherStoreID)
	}
	if !principal.HasScope("WAREHOUSE", warehouseID) {
		t.Errorf("expected HasScope true for assigned warehouse %s", warehouseID)
	}
	if !principal.HasScope("COMPANY", companyID) {
		t.Errorf("expected HasScope true for assigned company %s", companyID)
	}
}

func TestAuthenticatedPrincipal_ContextConversion(t *testing.T) {
	tenantID := uuid.New()
	userID := uuid.New()
	storeID := uuid.New()

	principal := &auth.AuthenticatedPrincipal{
		SessionID:     uuid.New(),
		TenantID:      tenantID,
		UserID:        userID,
		ActiveStoreID: storeID,
	}

	pc := principal.ToPrincipalContext()
	if pc.TenantID != tenantID || pc.UserID != userID || pc.StoreID != storeID {
		t.Errorf("unexpected PrincipalContext: %+v", pc)
	}

	ctx := auth.WithPrincipal(context.Background(), principal)
	retrieved, ok := auth.PrincipalFromContext(ctx)
	if !ok || retrieved == nil {
		t.Fatal("failed to retrieve principal from context")
	}
	if retrieved.UserID != userID {
		t.Errorf("expected user ID %s, got %s", userID, retrieved.UserID)
	}

	mustRetrieved, err := auth.MustPrincipalFromContext(ctx)
	if err != nil || mustRetrieved == nil {
		t.Fatalf("MustPrincipalFromContext returned error: %v", err)
	}

	_, err = auth.MustPrincipalFromContext(context.Background())
	if err == nil {
		t.Error("expected error for empty context in MustPrincipalFromContext")
	}
}
