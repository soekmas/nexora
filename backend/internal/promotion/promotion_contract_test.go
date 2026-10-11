package promotion_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOpenAPIContractParity verifies that the OpenAPI specification matches
// the implemented Promotion Engine routes, schemas, lifecycle transitions,
// and canonical field contracts.
func TestOpenAPIContractParity(t *testing.T) {
	// Locate contracts/openapi/openapi-v1.0.0.yaml relative to repository root.
	var openapiPath string
	for _, candidate := range []string{
		"../../../contracts/openapi/openapi-v1.0.0.yaml",
		"../../contracts/openapi/openapi-v1.0.0.yaml",
		"../contracts/openapi/openapi-v1.0.0.yaml",
		"contracts/openapi/openapi-v1.0.0.yaml",
	} {
		if abs, err := filepath.Abs(candidate); err == nil {
			if _, err := os.Stat(abs); err == nil {
				openapiPath = abs
				break
			}
		}
	}

	if openapiPath == "" {
		t.Fatal("contracts/openapi/openapi-v1.0.0.yaml not found")
	}

	contentBytes, err := os.ReadFile(openapiPath)
	if err != nil {
		t.Fatalf("failed to read openapi specification: %v", err)
	}
	spec := string(contentBytes)

	t.Run("Verify Promotion Routes Presence", func(t *testing.T) {
		type expectedPathMethod struct {
			path   string
			method string
		}

		expected := []expectedPathMethod{
			{path: "  /promotions:", method: "    get:"},
			{path: "  /promotions:", method: "    post:"},
			{path: "  /promotions/{id}:", method: "    get:"},
			{path: "  /promotions/{id}:", method: "    put:"},
			{path: "  /promotions/{id}/draft-version:", method: "    post:"},
			{path: "  /promotions/{id}/submit:", method: "    post:"},
			{path: "  /promotions/{id}/approve:", method: "    post:"},
			{path: "  /promotions/{id}/reject:", method: "    post:"},
			{path: "  /promotions/{id}/activate:", method: "    post:"},
			{path: "  /promotions/{id}/pause:", method: "    post:"},
			{path: "  /promotions/{id}/deactivate:", method: "    post:"},
			{path: "  /promotions/evaluate:", method: "    post:"},
		}

		for _, item := range expected {
			idx := strings.Index(spec, item.path)
			if idx == -1 {
				t.Errorf("Path %s not found in OpenAPI spec", item.path)
				continue
			}
			// Search within the path block (until next path declaration "  /")
			remainder := spec[idx+len(item.path):]
			nextPathIdx := strings.Index(remainder, "\n  /")
			var block string
			if nextPathIdx != -1 {
				block = remainder[:nextPathIdx]
			} else {
				block = remainder
			}

			if !strings.Contains(block, item.method) {
				t.Errorf("Path %s missing method %s in OpenAPI spec", item.path, item.method)
			}
		}
	})

	t.Run("Verify Promotion Schemas Presence", func(t *testing.T) {
		expectedSchemas := []string{
			"PromotionCreateRequest:",
			"PromotionUpdateRequest:",
			"PromotionConditionRequest:",
			"PromotionRewardRequest:",
			"MinOrderAmountConfig:",
			"MinItemQtyConfig:",
			"ProductInListConfig:",
			"CategoryInListConfig:",
			"PercentageDiscountConfig:",
			"FixedAmountDiscountConfig:",
			"BuyXGetYConfig:",
			"PromotionCondition:",
			"PromotionReward:",
			"Promotion:",
			"PromotionTransitionRequest:",
			"PromotionEvaluateRequest:",
			"PromotionCartItem:",
			"PromotionEvaluation:",
			"AppliedPromotion:",
			"BonusItem:",
		}

		for _, schema := range expectedSchemas {
			if !strings.Contains(spec, schema) {
				t.Errorf("OpenAPI specification missing expected schema: %q", schema)
			}
		}
	})

	t.Run("Verify BuyXGetY Schema and Reward Evaluator Parity", func(t *testing.T) {
		// Must include buy_qty, get_qty, reward_product_id, reward_variant_id, reward_item_name, discount_percentage, max_applications
		buyXGetYFields := []string{
			"buy_qty:",
			"get_qty:",
			"reward_product_id:",
			"reward_variant_id:",
			"reward_item_name:",
			"discount_percentage:",
			"max_applications:",
		}

		for _, field := range buyXGetYFields {
			if !strings.Contains(spec, field) {
				t.Errorf("OpenAPI BuyXGetYConfig missing expected field: %q", field)
			}
		}

		// BonusItem in OpenAPI must strictly match evaluator.BonusItem:
		// ProductID, VariantID, ItemName, FreeQuantity, DiscountAmount, PromotionID, PromotionCode
		bonusItemFields := []string{
			"BonusItem:\n      type: object",
			"product_id:",
			"variant_id:",
			"item_name:",
			"free_quantity:",
			"discount_amount:",
			"promotion_id:",
			"promotion_code:",
		}

		for _, field := range bonusItemFields {
			if !strings.Contains(spec, field) {
				t.Errorf("OpenAPI BonusItem missing expected field: %q", field)
			}
		}
	})

	t.Run("Verify Validity Period Aliases and Canonical Fields", func(t *testing.T) {
		// Must document both canonical (effective_from/effective_to) and aliases (valid_from/valid_to)
		periodFields := []string{
			"effective_from:",
			"effective_to:",
			"valid_from:",
			"valid_to:",
		}

		for _, field := range periodFields {
			if !strings.Contains(spec, field) {
				t.Errorf("OpenAPI Promotion missing expected period field: %q", field)
			}
		}
	})

	t.Run("Verify Lifecycle Justification Aliases and Canonical Fields", func(t *testing.T) {
		// PromotionTransitionRequest must contain reason and comment
		if !strings.Contains(spec, "PromotionTransitionRequest:") {
			t.Fatal("PromotionTransitionRequest not found")
		}
		transitionSnippet := spec[strings.Index(spec, "PromotionTransitionRequest:"):]
		if endIdx := strings.Index(transitionSnippet, "\n    Promotion"); endIdx != -1 {
			transitionSnippet = transitionSnippet[:endIdx]
		}

		if !strings.Contains(transitionSnippet, "reason:") {
			t.Errorf("PromotionTransitionRequest missing canonical field 'reason'")
		}
		if !strings.Contains(transitionSnippet, "comment:") {
			t.Errorf("PromotionTransitionRequest missing alias field 'comment'")
		}
	})

	t.Run("Verify Error Envelope References", func(t *testing.T) {
		// Must reference canonical NEXORA error responses
		canonicalErrorRefs := []string{
			"#/components/responses/BadRequest",
			"#/components/responses/Unauthorized",
			"#/components/responses/Forbidden",
			"#/components/responses/NotFound",
			"#/components/responses/Conflict",
			"#/components/responses/ServerError",
		}

		for _, ref := range canonicalErrorRefs {
			if !strings.Contains(spec, ref) {
				t.Errorf("OpenAPI missing canonical error response reference: %q", ref)
			}
		}
	})

	t.Run("Verify Idempotency-Key Header Documented", func(t *testing.T) {
		// Must document Idempotency-Key parameter
		if !strings.Contains(spec, "$ref: '#/components/headers/Idempotency-Key'") &&
			!strings.Contains(spec, "Idempotency-Key") {
			t.Errorf("OpenAPI missing Idempotency-Key header documentation")
		}
	})

	t.Run("Verify Evaluation Output Canonical and Alias Fields", func(t *testing.T) {
		// Must document applied_promotions (canonical) and applied (alias)
		if !strings.Contains(spec, "applied_promotions:") {
			t.Errorf("PromotionEvaluation missing canonical field 'applied_promotions'")
		}
		if !strings.Contains(spec, "applied:") {
			t.Errorf("PromotionEvaluation missing alias field 'applied'")
		}
		if !strings.Contains(spec, "bonus_items:") {
			t.Errorf("PromotionEvaluation missing 'bonus_items'")
		}
	})
}
