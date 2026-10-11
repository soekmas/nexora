// Package audit provides append-only, tenant-isolated audit logging and security event tracking.
package audit

import (
	"fmt"
	"strings"
)

// Severity levels defined in the audit action catalog and database constraints.
const (
	SeverityInfo     = "INFO"
	SeverityNotice   = "NOTICE"
	SeverityLow      = "LOW"
	SeverityMedium   = "MEDIUM"
	SeverityWarning  = "WARNING"
	SeverityHigh     = "HIGH"
	SeverityCritical = "CRITICAL"
)

// ActionDef models the specification of an approved audit action.
type ActionDef struct {
	Code           string
	Module         string
	Description    string
	Severity       string
	RequiresReason bool
}

// catalogRegistry contains all approved action codes from docs/architecture/13-audit-action-catalog.md
// and baseline seeds in db/migrations/000001_baseline_hardened.sql.
var catalogRegistry = map[string]ActionDef{
	// 11. Identity & Access
	"USER_CREATED":           {Code: "USER_CREATED", Module: "IDENTITY", Description: "User created", Severity: SeverityNotice, RequiresReason: false},
	"USER_UPDATED":           {Code: "USER_UPDATED", Module: "IDENTITY", Description: "User updated", Severity: SeverityNotice, RequiresReason: false},
	"USER_DISABLED":          {Code: "USER_DISABLED", Module: "IDENTITY", Description: "User disabled", Severity: SeverityHigh, RequiresReason: true},
	"USER_ENABLED":           {Code: "USER_ENABLED", Module: "IDENTITY", Description: "User enabled", Severity: SeverityNotice, RequiresReason: true},
	"USER_DELETED_REQUESTED": {Code: "USER_DELETED_REQUESTED", Module: "IDENTITY", Description: "User deletion requested", Severity: SeverityHigh, RequiresReason: true},
	"USER_ROLE_ASSIGNED":     {Code: "USER_ROLE_ASSIGNED", Module: "IDENTITY", Description: "User role assigned", Severity: SeverityHigh, RequiresReason: true},
	"USER_ROLE_REVOKED":      {Code: "USER_ROLE_REVOKED", Module: "IDENTITY", Description: "User role revoked", Severity: SeverityHigh, RequiresReason: true},
	"USER_ROLE_CHANGED":      {Code: "USER_ROLE_CHANGED", Module: "IDENTITY", Description: "User role changed", Severity: SeverityHigh, RequiresReason: true}, // Baseline seed alias
	"USER_SCOPE_CHANGED":     {Code: "USER_SCOPE_CHANGED", Module: "IDENTITY", Description: "User scope changed", Severity: SeverityHigh, RequiresReason: true},
	"PERMISSION_DENIED":      {Code: "PERMISSION_DENIED", Module: "SECURITY", Description: "Permission denied", Severity: SeverityHigh, RequiresReason: false},
	"LOGIN_SUCCESS":          {Code: "LOGIN_SUCCESS", Module: "SECURITY", Description: "Successful login", Severity: SeverityInfo, RequiresReason: false},
	"LOGIN_FAILURE":          {Code: "LOGIN_FAILURE", Module: "SECURITY", Description: "Failed login", Severity: SeverityWarning, RequiresReason: false},
	"MFA_SUCCESS":            {Code: "MFA_SUCCESS", Module: "SECURITY", Description: "MFA successful", Severity: SeverityInfo, RequiresReason: false},
	"MFA_FAILURE":            {Code: "MFA_FAILURE", Module: "SECURITY", Description: "MFA failure", Severity: SeverityHigh, RequiresReason: false},
	"SESSION_CREATED":        {Code: "SESSION_CREATED", Module: "SECURITY", Description: "Session created", Severity: SeverityInfo, RequiresReason: false},
	"SESSION_REVOKED":        {Code: "SESSION_REVOKED", Module: "SECURITY", Description: "Session revoked", Severity: SeverityHigh, RequiresReason: true},
	"API_KEY_CREATED":        {Code: "API_KEY_CREATED", Module: "SECURITY", Description: "API key created", Severity: SeverityHigh, RequiresReason: true},
	"API_KEY_REVOKED":        {Code: "API_KEY_REVOKED", Module: "SECURITY", Description: "API key revoked", Severity: SeverityHigh, RequiresReason: true},
	"BREAK_GLASS_GRANTED":    {Code: "BREAK_GLASS_GRANTED", Module: "SECURITY", Description: "Break glass access granted", Severity: SeverityCritical, RequiresReason: true},
	"BREAK_GLASS_EXPIRED":    {Code: "BREAK_GLASS_EXPIRED", Module: "SECURITY", Description: "Break glass access expired", Severity: SeverityHigh, RequiresReason: false},

	// 12. Organization
	"COMPANY_CREATED":     {Code: "COMPANY_CREATED", Module: "ORGANIZATION", Description: "Company created", Severity: SeverityNotice, RequiresReason: false},
	"COMPANY_UPDATED":     {Code: "COMPANY_UPDATED", Module: "ORGANIZATION", Description: "Company updated", Severity: SeverityNotice, RequiresReason: false},
	"REGION_CREATED":      {Code: "REGION_CREATED", Module: "ORGANIZATION", Description: "Region created", Severity: SeverityNotice, RequiresReason: false},
	"REGION_UPDATED":      {Code: "REGION_UPDATED", Module: "ORGANIZATION", Description: "Region updated", Severity: SeverityNotice, RequiresReason: false},
	"STORE_CREATED":       {Code: "STORE_CREATED", Module: "ORGANIZATION", Description: "Store created", Severity: SeverityNotice, RequiresReason: false},
	"STORE_UPDATED":       {Code: "STORE_UPDATED", Module: "ORGANIZATION", Description: "Store updated", Severity: SeverityNotice, RequiresReason: false},
	"STORE_ENABLED":       {Code: "STORE_ENABLED", Module: "ORGANIZATION", Description: "Store enabled", Severity: SeverityNotice, RequiresReason: false},
	"STORE_DISABLED":      {Code: "STORE_DISABLED", Module: "ORGANIZATION", Description: "Store disabled", Severity: SeverityHigh, RequiresReason: false},
	"WAREHOUSE_CREATED":   {Code: "WAREHOUSE_CREATED", Module: "ORGANIZATION", Description: "Warehouse created", Severity: SeverityNotice, RequiresReason: false},
	"WAREHOUSE_UPDATED":   {Code: "WAREHOUSE_UPDATED", Module: "ORGANIZATION", Description: "Warehouse updated", Severity: SeverityNotice, RequiresReason: false},
	"TERMINAL_REGISTERED": {Code: "TERMINAL_REGISTERED", Module: "ORGANIZATION", Description: "Terminal registered", Severity: SeverityNotice, RequiresReason: false},
	"TERMINAL_ENABLED":    {Code: "TERMINAL_ENABLED", Module: "ORGANIZATION", Description: "Terminal enabled", Severity: SeverityNotice, RequiresReason: false},
	"TERMINAL_DISABLED":   {Code: "TERMINAL_DISABLED", Module: "ORGANIZATION", Description: "Terminal disabled", Severity: SeverityHigh, RequiresReason: false},
	"TERMINAL_RETIRED":    {Code: "TERMINAL_RETIRED", Module: "ORGANIZATION", Description: "Terminal retired", Severity: SeverityHigh, RequiresReason: false},
	"DEVICE_REGISTERED":   {Code: "DEVICE_REGISTERED", Module: "ORGANIZATION", Description: "Device registered", Severity: SeverityNotice, RequiresReason: false},
	"DEVICE_SUSPENDED":    {Code: "DEVICE_SUSPENDED", Module: "ORGANIZATION", Description: "Device suspended", Severity: SeverityHigh, RequiresReason: false},
	"DEVICE_REVOKED":      {Code: "DEVICE_REVOKED", Module: "ORGANIZATION", Description: "Device revoked", Severity: SeverityCritical, RequiresReason: false},

	// 13. Master Data
	"PRODUCT_CREATED":         {Code: "PRODUCT_CREATED", Module: "MASTER", Description: "Product created", Severity: SeverityNotice, RequiresReason: false},
	"PRODUCT_UPDATED":         {Code: "PRODUCT_UPDATED", Module: "MASTER", Description: "Product updated", Severity: SeverityNotice, RequiresReason: false},
	"PRODUCT_ACTIVATED":       {Code: "PRODUCT_ACTIVATED", Module: "MASTER", Description: "Product activated", Severity: SeverityNotice, RequiresReason: false},
	"PRODUCT_DEACTIVATED":     {Code: "PRODUCT_DEACTIVATED", Module: "MASTER", Description: "Product deactivated", Severity: SeverityHigh, RequiresReason: false},
	"PRODUCT_VARIANT_CREATED": {Code: "PRODUCT_VARIANT_CREATED", Module: "MASTER", Description: "Product variant created", Severity: SeverityNotice, RequiresReason: false},
	"PRODUCT_VARIANT_UPDATED": {Code: "PRODUCT_VARIANT_UPDATED", Module: "MASTER", Description: "Product variant updated", Severity: SeverityNotice, RequiresReason: false},
	"BARCODE_CREATED":         {Code: "BARCODE_CREATED", Module: "MASTER", Description: "Barcode created", Severity: SeverityNotice, RequiresReason: false},
	"BARCODE_CHANGED":         {Code: "BARCODE_CHANGED", Module: "MASTER", Description: "Barcode changed", Severity: SeverityHigh, RequiresReason: false},
	"SERIAL_REGISTERED":       {Code: "SERIAL_REGISTERED", Module: "MASTER", Description: "Serial registered", Severity: SeverityNotice, RequiresReason: false},
	"SERIAL_STATUS_CHANGED":   {Code: "SERIAL_STATUS_CHANGED", Module: "MASTER", Description: "Serial status changed", Severity: SeverityHigh, RequiresReason: false},
	"CATEGORY_CREATED":        {Code: "CATEGORY_CREATED", Module: "MASTER", Description: "Category created", Severity: SeverityNotice, RequiresReason: false},
	"CATEGORY_UPDATED":        {Code: "CATEGORY_UPDATED", Module: "MASTER", Description: "Category updated", Severity: SeverityNotice, RequiresReason: false},
	"UOM_CREATED":             {Code: "UOM_CREATED", Module: "MASTER", Description: "Unit of measure created", Severity: SeverityNotice, RequiresReason: false},
	"UOM_UPDATED":             {Code: "UOM_UPDATED", Module: "MASTER", Description: "Unit of measure updated", Severity: SeverityNotice, RequiresReason: false},
	"BRAND_CREATED":           {Code: "BRAND_CREATED", Module: "MASTER", Description: "Brand created", Severity: SeverityNotice, RequiresReason: false},
	"BRAND_UPDATED":           {Code: "BRAND_UPDATED", Module: "MASTER", Description: "Brand updated", Severity: SeverityNotice, RequiresReason: false},

	// 14. Customer / CRM
	"CUSTOMER_CREATED":         {Code: "CUSTOMER_CREATED", Module: "CRM", Description: "Customer created", Severity: SeverityNotice, RequiresReason: false},
	"CUSTOMER_UPDATED":         {Code: "CUSTOMER_UPDATED", Module: "CRM", Description: "Customer updated", Severity: SeverityNotice, RequiresReason: false},
	"CUSTOMER_DISABLED":        {Code: "CUSTOMER_DISABLED", Module: "CRM", Description: "Customer disabled", Severity: SeverityHigh, RequiresReason: false},
	"CUSTOMER_ADDRESS_CREATED": {Code: "CUSTOMER_ADDRESS_CREATED", Module: "CRM", Description: "Customer address created", Severity: SeverityNotice, RequiresReason: false},
	"CUSTOMER_ADDRESS_UPDATED": {Code: "CUSTOMER_ADDRESS_UPDATED", Module: "CRM", Description: "Customer address updated", Severity: SeverityNotice, RequiresReason: false},
	"CUSTOMER_ADDRESS_DELETED": {Code: "CUSTOMER_ADDRESS_DELETED", Module: "CRM", Description: "Customer address deleted", Severity: SeverityHigh, RequiresReason: false},
	"CUSTOMER_GROUP_CHANGED":   {Code: "CUSTOMER_GROUP_CHANGED", Module: "CRM", Description: "Customer group changed", Severity: SeverityNotice, RequiresReason: false},
	"CUSTOMER_GROUP_CREATED":   {Code: "CUSTOMER_GROUP_CREATED", Module: "CRM", Description: "Customer group created", Severity: SeverityNotice, RequiresReason: false},
	"CUSTOMER_GROUP_UPDATED":   {Code: "CUSTOMER_GROUP_UPDATED", Module: "CRM", Description: "Customer group updated", Severity: SeverityNotice, RequiresReason: false},

	// 15. Pricing
	"PRICE_CREATED":     {Code: "PRICE_CREATED", Module: "PRICING", Description: "Price created", Severity: SeverityNotice, RequiresReason: false},
	"PRICE_UPDATED":     {Code: "PRICE_UPDATED", Module: "PRICING", Description: "Price updated", Severity: SeverityHigh, RequiresReason: true},
	"PRICE_CHANGED":     {Code: "PRICE_CHANGED", Module: "PRICING", Description: "Price changed", Severity: SeverityHigh, RequiresReason: true}, // Baseline seed alias
	"PRICE_SUBMITTED":   {Code: "PRICE_SUBMITTED", Module: "PRICING", Description: "Price submitted", Severity: SeverityNotice, RequiresReason: false},
	"PRICE_APPROVED":    {Code: "PRICE_APPROVED", Module: "PRICING", Description: "Price approved", Severity: SeverityHigh, RequiresReason: false},
	"PRICE_REJECTED":    {Code: "PRICE_REJECTED", Module: "PRICING", Description: "Price rejected", Severity: SeverityNotice, RequiresReason: false},
	"PRICE_ACTIVATED":   {Code: "PRICE_ACTIVATED", Module: "PRICING", Description: "Price activated", Severity: SeverityHigh, RequiresReason: false},
	"PRICE_DEACTIVATED": {Code: "PRICE_DEACTIVATED", Module: "PRICING", Description: "Price deactivated", Severity: SeverityHigh, RequiresReason: false},
	"PRICE_EXPIRED":     {Code: "PRICE_EXPIRED", Module: "PRICING", Description: "Price expired", Severity: SeverityInfo, RequiresReason: false},

	// 16. Promotion
	"PROMOTION_CREATED":         {Code: "PROMOTION_CREATED", Module: "PROMOTION", Description: "Promotion created", Severity: SeverityNotice, RequiresReason: false},
	"PROMOTION_UPDATED":         {Code: "PROMOTION_UPDATED", Module: "PROMOTION", Description: "Promotion updated", Severity: SeverityHigh, RequiresReason: true},
	"PROMOTION_SUBMITTED":       {Code: "PROMOTION_SUBMITTED", Module: "PROMOTION", Description: "Promotion submitted", Severity: SeverityNotice, RequiresReason: false},
	"PROMOTION_APPROVED":        {Code: "PROMOTION_APPROVED", Module: "PROMOTION", Description: "Promotion approved", Severity: SeverityHigh, RequiresReason: true},
	"PROMOTION_REJECTED":        {Code: "PROMOTION_REJECTED", Module: "PROMOTION", Description: "Promotion rejected", Severity: SeverityNotice, RequiresReason: false},
	"PROMOTION_ACTIVATED":       {Code: "PROMOTION_ACTIVATED", Module: "PROMOTION", Description: "Promotion activated", Severity: SeverityHigh, RequiresReason: false},
	"PROMOTION_PAUSED":          {Code: "PROMOTION_PAUSED", Module: "PROMOTION", Description: "Promotion paused", Severity: SeverityHigh, RequiresReason: false},
	"PROMOTION_EXPIRED":         {Code: "PROMOTION_EXPIRED", Module: "PROMOTION", Description: "Promotion expired", Severity: SeverityInfo, RequiresReason: false},
	"PROMOTION_VERSION_CREATED": {Code: "PROMOTION_VERSION_CREATED", Module: "PROMOTION", Description: "Promotion version created", Severity: SeverityNotice, RequiresReason: false},
	"PROMOTION_RULE_CHANGED":    {Code: "PROMOTION_RULE_CHANGED", Module: "PROMOTION", Description: "Promotion rule changed", Severity: SeverityHigh, RequiresReason: false},
	"PROMOTION_APPLIED":         {Code: "PROMOTION_APPLIED", Module: "PROMOTION", Description: "Promotion applied", Severity: SeverityInfo, RequiresReason: false},

	// 17. Sales
	"SALE_CREATED":                  {Code: "SALE_CREATED", Module: "SALES", Description: "Sale created", Severity: SeverityInfo, RequiresReason: false},
	"SALE_CONFIRMED":                {Code: "SALE_CONFIRMED", Module: "SALES", Description: "Sale confirmed", Severity: SeverityNotice, RequiresReason: false},
	"SALE_PROCESSING":               {Code: "SALE_PROCESSING", Module: "SALES", Description: "Sale processing", Severity: SeverityInfo, RequiresReason: false},
	"SALE_COMPLETED":                {Code: "SALE_COMPLETED", Module: "SALES", Description: "Sale completed", Severity: SeverityNotice, RequiresReason: false},
	"SALE_CANCELLED":                {Code: "SALE_CANCELLED", Module: "SALES", Description: "Sale cancelled", Severity: SeverityHigh, RequiresReason: true},
	"SALE_VOIDED":                   {Code: "SALE_VOIDED", Module: "SALES", Description: "Sale voided", Severity: SeverityCritical, RequiresReason: true},
	"SALE_REFUNDED":                 {Code: "SALE_REFUNDED", Module: "SALES", Description: "Sale refunded", Severity: SeverityHigh, RequiresReason: true}, // Baseline seed alias
	"SALE_ITEM_ADDED":               {Code: "SALE_ITEM_ADDED", Module: "SALES", Description: "Sale item added", Severity: SeverityInfo, RequiresReason: false},
	"SALE_ITEM_REMOVED":             {Code: "SALE_ITEM_REMOVED", Module: "SALES", Description: "Sale item removed", Severity: SeverityNotice, RequiresReason: false},
	"SALE_DISCOUNT_APPLIED":         {Code: "SALE_DISCOUNT_APPLIED", Module: "SALES", Description: "Sale discount applied", Severity: SeverityNotice, RequiresReason: false},
	"SALE_MANUAL_PRICE_OVERRIDE":    {Code: "SALE_MANUAL_PRICE_OVERRIDE", Module: "SALES", Description: "Manual price override", Severity: SeverityHigh, RequiresReason: true},
	"SALE_CUSTOMER_CHANGED":         {Code: "SALE_CUSTOMER_CHANGED", Module: "SALES", Description: "Sale customer changed", Severity: SeverityNotice, RequiresReason: false},
	"SALE_TAX_CHANGED":              {Code: "SALE_TAX_CHANGED", Module: "SALES", Description: "Sale tax changed", Severity: SeverityHigh, RequiresReason: true},
	"SALE_TOTAL_OVERRIDE_ATTEMPTED": {Code: "SALE_TOTAL_OVERRIDE_ATTEMPTED", Module: "SALES", Description: "Sale total override attempted", Severity: SeverityCritical, RequiresReason: false},

	// 18. POS Session / Cash
	"POS_SESSION_OPENED":            {Code: "POS_SESSION_OPENED", Module: "POS", Description: "POS session opened", Severity: SeverityNotice, RequiresReason: false},
	"POS_CASH_MOVEMENT_CREATED":     {Code: "POS_CASH_MOVEMENT_CREATED", Module: "POS", Description: "Cash movement created", Severity: SeverityNotice, RequiresReason: false},
	"POS_CASH_IN":                   {Code: "POS_CASH_IN", Module: "POS", Description: "Cash in recorded", Severity: SeverityNotice, RequiresReason: false},
	"POS_CASH_OUT":                  {Code: "POS_CASH_OUT", Module: "POS", Description: "Cash out recorded", Severity: SeverityNotice, RequiresReason: false},
	"POS_SESSION_CLOSING":           {Code: "POS_SESSION_CLOSING", Module: "POS", Description: "POS session closing", Severity: SeverityNotice, RequiresReason: false},
	"POS_SESSION_VARIANCE_DETECTED": {Code: "POS_SESSION_VARIANCE_DETECTED", Module: "POS", Description: "Cash variance detected", Severity: SeverityHigh, RequiresReason: false},
	"POS_SESSION_VARIANCE_APPROVED": {Code: "POS_SESSION_VARIANCE_APPROVED", Module: "POS", Description: "Cash variance approved", Severity: SeverityHigh, RequiresReason: true},
	"POS_SESSION_CLOSED":            {Code: "POS_SESSION_CLOSED", Module: "POS", Description: "POS session closed", Severity: SeverityHigh, RequiresReason: false},
	"POS_SESSION_FORCE_CLOSED":      {Code: "POS_SESSION_FORCE_CLOSED", Module: "POS", Description: "POS session force closed", Severity: SeverityCritical, RequiresReason: true},

	// 19. Payment
	"PAYMENT_CREATED":                 {Code: "PAYMENT_CREATED", Module: "PAYMENT", Description: "Payment created", Severity: SeverityNotice, RequiresReason: false},
	"PAYMENT_PROCESSING":              {Code: "PAYMENT_PROCESSING", Module: "PAYMENT", Description: "Payment processing", Severity: SeverityInfo, RequiresReason: false},
	"PAYMENT_COMPLETED":               {Code: "PAYMENT_COMPLETED", Module: "PAYMENT", Description: "Payment completed", Severity: SeverityNotice, RequiresReason: false},
	"PAYMENT_SUCCESS":                 {Code: "PAYMENT_SUCCESS", Module: "PAYMENT", Description: "Payment successful", Severity: SeverityNotice, RequiresReason: false}, // Baseline seed alias
	"PAYMENT_FAILED":                  {Code: "PAYMENT_FAILED", Module: "PAYMENT", Description: "Payment failed", Severity: SeverityWarning, RequiresReason: false},
	"PAYMENT_CANCELLED":               {Code: "PAYMENT_CANCELLED", Module: "PAYMENT", Description: "Payment cancelled", Severity: SeverityNotice, RequiresReason: false},
	"PAYMENT_EXPIRED":                 {Code: "PAYMENT_EXPIRED", Module: "PAYMENT", Description: "Payment expired", Severity: SeverityNotice, RequiresReason: false},
	"PAYMENT_UNKNOWN":                 {Code: "PAYMENT_UNKNOWN", Module: "PAYMENT", Description: "Payment ambiguous/unknown", Severity: SeverityCritical, RequiresReason: false},
	"PAYMENT_RECONCILIATION_STARTED":  {Code: "PAYMENT_RECONCILIATION_STARTED", Module: "PAYMENT", Description: "Payment reconciliation started", Severity: SeverityHigh, RequiresReason: false},
	"PAYMENT_RECONCILIATION_RESOLVED": {Code: "PAYMENT_RECONCILIATION_RESOLVED", Module: "PAYMENT", Description: "Payment reconciliation resolved", Severity: SeverityHigh, RequiresReason: true},
	"REFUND_REQUESTED":                {Code: "REFUND_REQUESTED", Module: "PAYMENT", Description: "Refund requested", Severity: SeverityHigh, RequiresReason: false},
	"REFUND_APPROVED":                 {Code: "REFUND_APPROVED", Module: "PAYMENT", Description: "Refund approved", Severity: SeverityHigh, RequiresReason: true},
	"REFUND_PROCESSING":               {Code: "REFUND_PROCESSING", Module: "PAYMENT", Description: "Refund processing", Severity: SeverityHigh, RequiresReason: false},
	"REFUND_COMPLETED":                {Code: "REFUND_COMPLETED", Module: "PAYMENT", Description: "Refund completed", Severity: SeverityHigh, RequiresReason: false},
	"PAYMENT_REFUNDED":                {Code: "PAYMENT_REFUNDED", Module: "PAYMENT", Description: "Payment refunded", Severity: SeverityHigh, RequiresReason: true}, // Baseline seed alias
	"REFUND_FAILED":                   {Code: "REFUND_FAILED", Module: "PAYMENT", Description: "Refund failed", Severity: SeverityHigh, RequiresReason: false},
	"REFUND_CANCELLED":                {Code: "REFUND_CANCELLED", Module: "PAYMENT", Description: "Refund cancelled", Severity: SeverityHigh, RequiresReason: false},

	// 20. Inventory
	"STOCK_RESERVED":             {Code: "STOCK_RESERVED", Module: "INVENTORY", Description: "Stock reserved", Severity: SeverityInfo, RequiresReason: false},
	"STOCK_RELEASED":             {Code: "STOCK_RELEASED", Module: "INVENTORY", Description: "Stock released", Severity: SeverityInfo, RequiresReason: false},
	"STOCK_DEDUCTED":             {Code: "STOCK_DEDUCTED", Module: "INVENTORY", Description: "Stock deducted", Severity: SeverityNotice, RequiresReason: false},
	"STOCK_ADJUSTMENT_CREATED":   {Code: "STOCK_ADJUSTMENT_CREATED", Module: "INVENTORY", Description: "Stock adjustment created", Severity: SeverityHigh, RequiresReason: false},
	"STOCK_ADJUSTMENT_SUBMITTED": {Code: "STOCK_ADJUSTMENT_SUBMITTED", Module: "INVENTORY", Description: "Stock adjustment submitted", Severity: SeverityNotice, RequiresReason: false},
	"STOCK_ADJUSTMENT_APPROVED":  {Code: "STOCK_ADJUSTMENT_APPROVED", Module: "INVENTORY", Description: "Stock adjustment approved", Severity: SeverityHigh, RequiresReason: true},
	"STOCK_ADJUSTMENT_REJECTED":  {Code: "STOCK_ADJUSTMENT_REJECTED", Module: "INVENTORY", Description: "Stock adjustment rejected", Severity: SeverityNotice, RequiresReason: false},
	"STOCK_ADJUSTED":             {Code: "STOCK_ADJUSTED", Module: "INVENTORY", Description: "Stock adjusted", Severity: SeverityHigh, RequiresReason: true},
	"STOCK_TRANSFER_CREATED":     {Code: "STOCK_TRANSFER_CREATED", Module: "INVENTORY", Description: "Stock transfer created", Severity: SeverityNotice, RequiresReason: false},
	"STOCK_TRANSFER_APPROVED":    {Code: "STOCK_TRANSFER_APPROVED", Module: "INVENTORY", Description: "Stock transfer approved", Severity: SeverityHigh, RequiresReason: false},
	"STOCK_TRANSFER_SENT":        {Code: "STOCK_TRANSFER_SENT", Module: "INVENTORY", Description: "Stock transfer dispatched", Severity: SeverityNotice, RequiresReason: false},
	"STOCK_TRANSFER_RECEIVED":    {Code: "STOCK_TRANSFER_RECEIVED", Module: "INVENTORY", Description: "Stock transfer received", Severity: SeverityHigh, RequiresReason: false},
	"STOCK_TRANSFERRED":          {Code: "STOCK_TRANSFERRED", Module: "INVENTORY", Description: "Stock transferred", Severity: SeverityInfo, RequiresReason: false}, // Baseline seed alias
	"STOCK_OPNAME_STARTED":       {Code: "STOCK_OPNAME_STARTED", Module: "INVENTORY", Description: "Stock opname started", Severity: SeverityNotice, RequiresReason: false},
	"STOCK_OPNAME_POSTED":        {Code: "STOCK_OPNAME_POSTED", Module: "INVENTORY", Description: "Stock opname posted", Severity: SeverityHigh, RequiresReason: true},
	"STOCK_NEGATIVE_ATTEMPT":     {Code: "STOCK_NEGATIVE_ATTEMPT", Module: "INVENTORY", Description: "Negative stock attempt blocked", Severity: SeverityCritical, RequiresReason: false},

	// 21. Purchasing
	"PURCHASE_ORDER_CREATED":   {Code: "PURCHASE_ORDER_CREATED", Module: "PURCHASING", Description: "Purchase order created", Severity: SeverityInfo, RequiresReason: false},
	"PURCHASE_ORDER_UPDATED":   {Code: "PURCHASE_ORDER_UPDATED", Module: "PURCHASING", Description: "Purchase order updated", Severity: SeverityNotice, RequiresReason: false},
	"PURCHASE_ORDER_SUBMITTED": {Code: "PURCHASE_ORDER_SUBMITTED", Module: "PURCHASING", Description: "Purchase order submitted", Severity: SeverityNotice, RequiresReason: false},
	"PURCHASE_ORDER_APPROVED":  {Code: "PURCHASE_ORDER_APPROVED", Module: "PURCHASING", Description: "Purchase order approved", Severity: SeverityHigh, RequiresReason: false},
	"PURCHASE_ORDER_REJECTED":  {Code: "PURCHASE_ORDER_REJECTED", Module: "PURCHASING", Description: "Purchase order rejected", Severity: SeverityNotice, RequiresReason: false},
	"PURCHASE_ORDER_CANCELLED": {Code: "PURCHASE_ORDER_CANCELLED", Module: "PURCHASING", Description: "Purchase order cancelled", Severity: SeverityHigh, RequiresReason: true},
	"GOODS_RECEIPT_CREATED":    {Code: "GOODS_RECEIPT_CREATED", Module: "PURCHASING", Description: "Goods receipt created", Severity: SeverityInfo, RequiresReason: false},
	"GOODS_RECEIPT_POSTED":     {Code: "GOODS_RECEIPT_POSTED", Module: "PURCHASING", Description: "Goods receipt posted", Severity: SeverityHigh, RequiresReason: false},
	"GOODS_RECEIPT_CANCELLED":  {Code: "GOODS_RECEIPT_CANCELLED", Module: "PURCHASING", Description: "Goods receipt cancelled", Severity: SeverityHigh, RequiresReason: true},

	// 22. Loyalty / Voucher
	"LOYALTY_ACCOUNT_CREATED": {Code: "LOYALTY_ACCOUNT_CREATED", Module: "LOYALTY", Description: "Loyalty account created", Severity: SeverityNotice, RequiresReason: false},
	"LOYALTY_EARNED":          {Code: "LOYALTY_EARNED", Module: "LOYALTY", Description: "Loyalty points earned", Severity: SeverityNotice, RequiresReason: false},
	"LOYALTY_REDEEMED":        {Code: "LOYALTY_REDEEMED", Module: "LOYALTY", Description: "Loyalty points redeemed", Severity: SeverityHigh, RequiresReason: false},
	"LOYALTY_REVERSED":        {Code: "LOYALTY_REVERSED", Module: "LOYALTY", Description: "Loyalty points reversed", Severity: SeverityHigh, RequiresReason: true},
	"VOUCHER_ISSUED":          {Code: "VOUCHER_ISSUED", Module: "LOYALTY", Description: "Voucher issued", Severity: SeverityNotice, RequiresReason: false},
	"VOUCHER_RESERVED":        {Code: "VOUCHER_RESERVED", Module: "LOYALTY", Description: "Voucher reserved", Severity: SeverityNotice, RequiresReason: false},
	"VOUCHER_REDEEMED":        {Code: "VOUCHER_REDEEMED", Module: "LOYALTY", Description: "Voucher redeemed", Severity: SeverityHigh, RequiresReason: false},
	"VOUCHER_RELEASED":        {Code: "VOUCHER_RELEASED", Module: "LOYALTY", Description: "Voucher released", Severity: SeverityNotice, RequiresReason: false},
	"VOUCHER_CANCELLED":       {Code: "VOUCHER_CANCELLED", Module: "LOYALTY", Description: "Voucher cancelled", Severity: SeverityHigh, RequiresReason: true},
	"VOUCHER_EXPIRED":         {Code: "VOUCHER_EXPIRED", Module: "LOYALTY", Description: "Voucher expired", Severity: SeverityInfo, RequiresReason: false},

	// 23. Workflow / Approval
	"WORKFLOW_STARTED":         {Code: "WORKFLOW_STARTED", Module: "WORKFLOW", Description: "Workflow started", Severity: SeverityInfo, RequiresReason: false},
	"APPROVAL_REQUESTED":       {Code: "APPROVAL_REQUESTED", Module: "WORKFLOW", Description: "Approval requested", Severity: SeverityNotice, RequiresReason: false},
	"APPROVAL_APPROVED":        {Code: "APPROVAL_APPROVED", Module: "WORKFLOW", Description: "Approval approved", Severity: SeverityHigh, RequiresReason: false},
	"APPROVAL_REJECTED":        {Code: "APPROVAL_REJECTED", Module: "WORKFLOW", Description: "Approval rejected", Severity: SeverityNotice, RequiresReason: true},
	"APPROVAL_CANCELLED":       {Code: "APPROVAL_CANCELLED", Module: "WORKFLOW", Description: "Approval cancelled", Severity: SeverityNotice, RequiresReason: false},
	"APPROVAL_EXPIRED":         {Code: "APPROVAL_EXPIRED", Module: "WORKFLOW", Description: "Approval expired", Severity: SeverityNotice, RequiresReason: false},
	"WORKFLOW_COMPLETED":       {Code: "WORKFLOW_COMPLETED", Module: "WORKFLOW", Description: "Workflow completed", Severity: SeverityInfo, RequiresReason: false},
	"WORKFLOW_FORCE_COMPLETED": {Code: "WORKFLOW_FORCE_COMPLETED", Module: "WORKFLOW", Description: "Workflow force completed", Severity: SeverityCritical, RequiresReason: true},

	// 24. Integration / ERP
	"INTEGRATION_REQUESTED":        {Code: "INTEGRATION_REQUESTED", Module: "INTEGRATION", Description: "Integration requested", Severity: SeverityInfo, RequiresReason: false},
	"INTEGRATION_SENT":             {Code: "INTEGRATION_SENT", Module: "INTEGRATION", Description: "Integration message sent", Severity: SeverityInfo, RequiresReason: false},
	"INTEGRATION_SUCCEEDED":        {Code: "INTEGRATION_SUCCEEDED", Module: "INTEGRATION", Description: "Integration succeeded", Severity: SeverityNotice, RequiresReason: false},
	"INTEGRATION_FAILED_RETRYABLE": {Code: "INTEGRATION_FAILED_RETRYABLE", Module: "INTEGRATION", Description: "Integration failed retryable", Severity: SeverityWarning, RequiresReason: false},
	"INTEGRATION_FAILED_PERMANENT": {Code: "INTEGRATION_FAILED_PERMANENT", Module: "INTEGRATION", Description: "Integration failed permanent", Severity: SeverityHigh, RequiresReason: false},
	"INTEGRATION_DEAD_LETTERED":    {Code: "INTEGRATION_DEAD_LETTERED", Module: "INTEGRATION", Description: "Integration dead-lettered", Severity: SeverityHigh, RequiresReason: false},
	"ERP_DOCUMENT_CREATED":         {Code: "ERP_DOCUMENT_CREATED", Module: "INTEGRATION", Description: "ERP document created", Severity: SeverityNotice, RequiresReason: false},
	"ERP_DOCUMENT_LINK_FAILED":     {Code: "ERP_DOCUMENT_LINK_FAILED", Module: "INTEGRATION", Description: "ERP document link failed", Severity: SeverityHigh, RequiresReason: false},
	"ERP_RECONCILIATION_REQUIRED":  {Code: "ERP_RECONCILIATION_REQUIRED", Module: "INTEGRATION", Description: "ERP reconciliation required", Severity: SeverityHigh, RequiresReason: false},
	"ERP_RECONCILIATION_RESOLVED":  {Code: "ERP_RECONCILIATION_RESOLVED", Module: "INTEGRATION", Description: "ERP reconciliation resolved", Severity: SeverityHigh, RequiresReason: true},
	"EXTERNAL_REFERENCE_LINKED":    {Code: "EXTERNAL_REFERENCE_LINKED", Module: "INTEGRATION", Description: "External reference linked", Severity: SeverityInfo, RequiresReason: false},

	// 25. Offline Sync
	"OFFLINE_TRANSACTION_QUEUED":                  {Code: "OFFLINE_TRANSACTION_QUEUED", Module: "OFFLINE", Description: "Offline transaction queued", Severity: SeverityInfo, RequiresReason: false},
	"OFFLINE_TRANSACTION_SYNC_STARTED":            {Code: "OFFLINE_TRANSACTION_SYNC_STARTED", Module: "OFFLINE", Description: "Offline sync started", Severity: SeverityInfo, RequiresReason: false},
	"OFFLINE_TRANSACTION_SYNCED":                  {Code: "OFFLINE_TRANSACTION_SYNCED", Module: "OFFLINE", Description: "Offline transaction synced", Severity: SeverityNotice, RequiresReason: false},
	"OFFLINE_TRANSACTION_RETRYABLE_FAILURE":       {Code: "OFFLINE_TRANSACTION_RETRYABLE_FAILURE", Module: "OFFLINE", Description: "Offline retryable failure", Severity: SeverityWarning, RequiresReason: false},
	"OFFLINE_TRANSACTION_CONFLICT":                {Code: "OFFLINE_TRANSACTION_CONFLICT", Module: "OFFLINE", Description: "Offline sync conflict", Severity: SeverityHigh, RequiresReason: false},
	"OFFLINE_TRANSACTION_RECONCILIATION_REQUIRED": {Code: "OFFLINE_TRANSACTION_RECONCILIATION_REQUIRED", Module: "OFFLINE", Description: "Offline reconciliation required", Severity: SeverityHigh, RequiresReason: false},
	"OFFLINE_TRANSACTION_RESOLVED":                {Code: "OFFLINE_TRANSACTION_RESOLVED", Module: "OFFLINE", Description: "Offline sync resolved", Severity: SeverityHigh, RequiresReason: true},
	"OFFLINE_POLICY_EXPIRED":                      {Code: "OFFLINE_POLICY_EXPIRED", Module: "OFFLINE", Description: "Offline policy expired", Severity: SeverityWarning, RequiresReason: false},
	"DEVICE_CLOCK_DRIFT_DETECTED":                 {Code: "DEVICE_CLOCK_DRIFT_DETECTED", Module: "OFFLINE", Description: "Device clock drift detected", Severity: SeverityWarning, RequiresReason: false},
	"DEVICE_REVOKED_DURING_SYNC":                  {Code: "DEVICE_REVOKED_DURING_SYNC", Module: "OFFLINE", Description: "Device revoked during sync", Severity: SeverityCritical, RequiresReason: false},
	"IDEMPOTENCY_KEY_REUSED":                      {Code: "IDEMPOTENCY_KEY_REUSED", Module: "PLATFORM", Description: "Idempotency key reused with different payload", Severity: SeverityHigh, RequiresReason: false},

	// 26. Security Telemetry
	"RATE_LIMIT_TRIGGERED":      {Code: "RATE_LIMIT_TRIGGERED", Module: "SECURITY", Description: "Rate limit triggered", Severity: SeverityWarning, RequiresReason: false},
	"SUSPICIOUS_AUTH_ACTIVITY":  {Code: "SUSPICIOUS_AUTH_ACTIVITY", Module: "SECURITY", Description: "Suspicious auth activity", Severity: SeverityHigh, RequiresReason: false},
	"ROLE_CHANGED":              {Code: "ROLE_CHANGED", Module: "SECURITY", Description: "Role permissions changed", Severity: SeverityHigh, RequiresReason: true},
	"SCOPE_CHANGED":             {Code: "SCOPE_CHANGED", Module: "SECURITY", Description: "Scope assigned or modified", Severity: SeverityHigh, RequiresReason: true},
	"TENANT_ACCESS_DENIED":      {Code: "TENANT_ACCESS_DENIED", Module: "SECURITY", Description: "Cross-tenant access attempt denied", Severity: SeverityCritical, RequiresReason: false},
	"CROSS_SCOPE_ACCESS_DENIED": {Code: "CROSS_SCOPE_ACCESS_DENIED", Module: "SECURITY", Description: "Cross-scope access attempt denied", Severity: SeverityHigh, RequiresReason: false},
	"RLS_CONTEXT_MISSING":       {Code: "RLS_CONTEXT_MISSING", Module: "SECURITY", Description: "RLS context missing on database transaction", Severity: SeverityCritical, RequiresReason: false},

	// 27. Audit Access
	"AUDIT_SEARCHED":           {Code: "AUDIT_SEARCHED", Module: "AUDIT", Description: "Audit search executed", Severity: SeverityInfo, RequiresReason: false},
	"AUDIT_RECORD_VIEWED":      {Code: "AUDIT_RECORD_VIEWED", Module: "AUDIT", Description: "Audit record inspected", Severity: SeverityNotice, RequiresReason: false},
	"AUDIT_EXPORTED":           {Code: "AUDIT_EXPORTED", Module: "AUDIT", Description: "Audit export performed", Severity: SeverityHigh, RequiresReason: true},
	"AUDIT_EXPORT_FAILED":      {Code: "AUDIT_EXPORT_FAILED", Module: "AUDIT", Description: "Audit export failed", Severity: SeverityWarning, RequiresReason: false},
	"AUDIT_REPLAY_REQUESTED":   {Code: "AUDIT_REPLAY_REQUESTED", Module: "AUDIT", Description: "Audit replay requested", Severity: SeverityHigh, RequiresReason: true},
	"AUDIT_RETENTION_EXECUTED": {Code: "AUDIT_RETENTION_EXECUTED", Module: "AUDIT", Description: "Audit retention executed", Severity: SeverityHigh, RequiresReason: true},
	"AUDIT_ACCESS_DENIED":      {Code: "AUDIT_ACCESS_DENIED", Module: "AUDIT", Description: "Audit access denied", Severity: SeverityHigh, RequiresReason: false},
}

// LookupAction returns the ActionDef for the given action code, or false if not registered.
func LookupAction(code string) (ActionDef, bool) {
	norm := strings.ToUpper(strings.TrimSpace(code))
	def, exists := catalogRegistry[norm]
	return def, exists
}

// ValidateAction verifies that the action code exists in the approved catalog.
// Returns ErrInvalidAction if unrecognized.
func ValidateAction(code string) (ActionDef, error) {
	def, exists := LookupAction(code)
	if !exists {
		return ActionDef{}, fmt.Errorf("%w: '%s'", ErrInvalidAction, code)
	}
	return def, nil
}
