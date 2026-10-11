-- Migration 000006: Canonical Permission Catalog Seed
-- Seeds canonical system permissions defined in docs/architecture/11-permission-matrix.md
-- into identity.permission.

BEGIN;

SELECT pg_advisory_xact_lock(hashtext('nexora.identity.permissions.v1'));

INSERT INTO identity.permission (code, name, module, action) VALUES
  -- 1. Identity & Access
  ('user.read', 'Read User Account', 'IDENTITY', 'USER_READ'),
  ('user.create', 'Create User Account', 'IDENTITY', 'USER_CREATE'),
  ('user.update', 'Update User Account', 'IDENTITY', 'USER_UPDATE'),
  ('user.disable', 'Disable User Account', 'IDENTITY', 'USER_DISABLE'),
  ('user.unlock', 'Unlock User Account', 'IDENTITY', 'USER_UNLOCK'),
  ('user.role.read', 'Read User Role Assignments', 'IDENTITY', 'USER_ROLE_READ'),
  ('user.role.assign', 'Assign Role to User', 'IDENTITY', 'USER_ROLE_ASSIGN'),
  ('user.role.revoke', 'Revoke Role from User', 'IDENTITY', 'USER_ROLE_REVOKE'),
  ('user.scope.manage', 'Manage User Scopes', 'IDENTITY', 'USER_SCOPE_MANAGE'),
  ('role.read', 'Read Role Definition', 'IDENTITY', 'ROLE_READ'),
  ('role.create', 'Create Role', 'IDENTITY', 'ROLE_CREATE'),
  ('role.update', 'Update Role', 'IDENTITY', 'ROLE_UPDATE'),
  ('role.permission.assign', 'Assign Permission to Role', 'IDENTITY', 'ROLE_PERMISSION_ASSIGN'),
  ('mfa.manage', 'Manage Multi-Factor Authentication', 'IDENTITY', 'MFA_MANAGE'),

  -- 2. Security & Session
  ('session.revoke', 'Revoke Active Session', 'SECURITY', 'SESSION_REVOKE'),
  ('security_event.read', 'Read Security Audit Events', 'SECURITY', 'SECURITY_EVENT_READ'),
  ('login_history.read', 'Read User Login History', 'SECURITY', 'LOGIN_HISTORY_READ'),

  -- 3. Organization (Metadata Catalog)
  ('company.read', 'Read Company', 'ORGANIZATION', 'COMPANY_READ'),
  ('company.create', 'Create Company', 'ORGANIZATION', 'COMPANY_CREATE'),
  ('company.update', 'Update Company', 'ORGANIZATION', 'COMPANY_UPDATE'),
  ('region.read', 'Read Region', 'ORGANIZATION', 'REGION_READ'),
  ('region.create', 'Create Region', 'ORGANIZATION', 'REGION_CREATE'),
  ('region.update', 'Update Region', 'ORGANIZATION', 'REGION_UPDATE'),
  ('store.read', 'Read Store', 'ORGANIZATION', 'STORE_READ'),
  ('store.create', 'Create Store', 'ORGANIZATION', 'STORE_CREATE'),
  ('store.update', 'Update Store', 'ORGANIZATION', 'STORE_UPDATE'),
  ('store.disable', 'Disable Store', 'ORGANIZATION', 'STORE_DISABLE'),
  ('warehouse.read', 'Read Warehouse', 'ORGANIZATION', 'WAREHOUSE_READ'),
  ('warehouse.create', 'Create Warehouse', 'ORGANIZATION', 'WAREHOUSE_CREATE'),
  ('warehouse.update', 'Update Warehouse', 'ORGANIZATION', 'WAREHOUSE_UPDATE'),
  ('terminal.read', 'Read POS Terminal', 'ORGANIZATION', 'TERMINAL_READ'),
  ('terminal.register', 'Register POS Terminal', 'ORGANIZATION', 'TERMINAL_REGISTER'),
  ('terminal.update', 'Update POS Terminal', 'ORGANIZATION', 'TERMINAL_UPDATE'),
  ('terminal.disable', 'Disable POS Terminal', 'ORGANIZATION', 'TERMINAL_DISABLE'),
  ('terminal.retire', 'Retire POS Terminal', 'ORGANIZATION', 'TERMINAL_RETIRE'),

  -- 4. Master Data (Metadata Catalog)
  ('product.read', 'Read Product', 'MASTER', 'PRODUCT_READ'),
  ('product.create', 'Create Product', 'MASTER', 'PRODUCT_CREATE'),
  ('product.update', 'Update Product', 'MASTER', 'PRODUCT_UPDATE'),
  ('product.activate', 'Activate Product', 'MASTER', 'PRODUCT_ACTIVATE'),
  ('product.deactivate', 'Deactivate Product', 'MASTER', 'PRODUCT_DEACTIVATE'),
  ('product.barcode.read', 'Read Product Barcode', 'MASTER', 'BARCODE_READ'),
  ('product.barcode.manage', 'Manage Product Barcodes', 'MASTER', 'BARCODE_MANAGE'),
  ('serial.read', 'Read Serial Number', 'MASTER', 'SERIAL_READ'),
  ('serial.manage', 'Manage Serial Numbers', 'MASTER', 'SERIAL_MANAGE'),
  ('uom.read', 'Read Unit of Measure', 'MASTER', 'UOM_READ'),
  ('category.read', 'Read Product Category', 'MASTER', 'CATEGORY_READ'),
  ('brand.read', 'Read Product Brand', 'MASTER', 'BRAND_READ'),

  -- 5. CRM (Metadata Catalog)
  ('customer.read', 'Read Customer', 'CRM', 'CUSTOMER_READ'),
  ('customer.create', 'Create Customer', 'CRM', 'CUSTOMER_CREATE'),
  ('customer.update', 'Update Customer', 'CRM', 'CUSTOMER_UPDATE'),
  ('customer.address.manage', 'Manage Customer Addresses', 'CRM', 'ADDRESS_MANAGE'),

  -- 6. Sales (Metadata Catalog)
  ('sales.order.read', 'Read Sales Order', 'SALES', 'ORDER_READ'),
  ('sales.order.create', 'Create Sales Order', 'SALES', 'ORDER_CREATE'),
  ('sales.order.confirm', 'Confirm Sales Order', 'SALES', 'ORDER_CONFIRM'),
  ('sales.order.cancel', 'Cancel Sales Order', 'SALES', 'ORDER_CANCEL'),
  ('sales.order.void', 'Void Sales Order', 'SALES', 'ORDER_VOID'),
  ('sales.refund.request', 'Request Sales Refund', 'SALES', 'REFUND_REQUEST'),
  ('sales.refund.approve', 'Approve Sales Refund', 'SALES', 'REFUND_APPROVE'),
  ('sales.refund.read', 'Read Sales Refund', 'SALES', 'REFUND_READ'),
  ('sales.export', 'Export Sales Data', 'SALES', 'SALES_EXPORT'),

  -- 7. Payment (Metadata Catalog)
  ('payment.read', 'Read Payment', 'PAYMENT', 'PAYMENT_READ'),
  ('payment.create', 'Create Payment', 'PAYMENT', 'PAYMENT_CREATE'),
  ('payment.process', 'Process Payment', 'PAYMENT', 'PAYMENT_PROCESS'),
  ('payment.reconcile', 'Reconcile Payment', 'PAYMENT', 'PAYMENT_RECONCILE'),
  ('payment.refund.request', 'Request Payment Refund', 'PAYMENT', 'REFUND_REQUEST'),
  ('payment.refund.approve', 'Approve Payment Refund', 'PAYMENT', 'REFUND_APPROVE'),
  ('payment.provider.manage', 'Manage Payment Providers', 'PAYMENT', 'PROVIDER_MANAGE'),

  -- 8. POS Session (Metadata Catalog)
  ('session.read', 'Read POS Session', 'POS', 'SESSION_READ'),
  ('session.open', 'Open POS Session', 'POS', 'SESSION_OPEN'),
  ('session.close', 'Close POS Session', 'POS', 'SESSION_CLOSE'),
  ('session.variance.review', 'Review Session Variance', 'POS', 'VARIANCE_REVIEW'),
  ('session.variance.approve', 'Approve Session Variance', 'POS', 'VARIANCE_APPROVE'),
  ('cash_movement.create', 'Record Cash Movement', 'POS', 'CASH_MOVE_CREATE'),
  ('cash_movement.approve', 'Approve Cash Movement', 'POS', 'CASH_MOVE_APPROVE'),

  -- 9. Inventory (Metadata Catalog)
  ('inventory.read', 'Read Inventory Stock', 'INVENTORY', 'STOCK_READ'),
  ('inventory.reserve', 'Reserve Inventory Stock', 'INVENTORY', 'STOCK_RESERVE'),
  ('inventory.adjust.read', 'Read Inventory Adjustments', 'INVENTORY', 'ADJUST_READ'),
  ('inventory.adjust.create', 'Create Inventory Adjustment', 'INVENTORY', 'ADJUST_CREATE'),
  ('inventory.adjust.submit', 'Submit Inventory Adjustment', 'INVENTORY', 'ADJUST_SUBMIT'),
  ('inventory.adjust.approve', 'Approve Inventory Adjustment', 'INVENTORY', 'ADJUST_APPROVE')
ON CONFLICT (code) DO NOTHING;

COMMIT;
