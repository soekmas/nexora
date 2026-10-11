-- POS Platform PostgreSQL Database Design v1
-- Enterprise / Odoo-ready / Multi-company / Multi-store
-- Target: PostgreSQL 16+
-- Strategy: Modular monolith, API-first, event-ready
-- Security: RBAC + scoped access + PostgreSQL RLS + immutable ledgers + audit

BEGIN;

CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS citext;

-- ============================================================
-- 0. CORE SCHEMAS
-- ============================================================
CREATE SCHEMA IF NOT EXISTS identity;
CREATE SCHEMA IF NOT EXISTS organization;
CREATE SCHEMA IF NOT EXISTS master;
CREATE SCHEMA IF NOT EXISTS crm;
CREATE SCHEMA IF NOT EXISTS pricing;
CREATE SCHEMA IF NOT EXISTS promotion;
CREATE SCHEMA IF NOT EXISTS sales;
CREATE SCHEMA IF NOT EXISTS pos;
CREATE SCHEMA IF NOT EXISTS payment;
CREATE SCHEMA IF NOT EXISTS inventory;
CREATE SCHEMA IF NOT EXISTS warehouse;
CREATE SCHEMA IF NOT EXISTS purchasing;
CREATE SCHEMA IF NOT EXISTS finance;
CREATE SCHEMA IF NOT EXISTS loyalty;
CREATE SCHEMA IF NOT EXISTS workflow;
CREATE SCHEMA IF NOT EXISTS integration;
CREATE SCHEMA IF NOT EXISTS audit;
CREATE SCHEMA IF NOT EXISTS platform;
CREATE SCHEMA IF NOT EXISTS reporting;

-- ============================================================
-- 1. COMMON FUNCTIONS / TYPES
-- ============================================================
CREATE OR REPLACE FUNCTION platform.touch_updated_at()
RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION platform.current_tenant_id()
RETURNS uuid
LANGUAGE sql STABLE AS $$
  SELECT NULLIF(current_setting('app.tenant_id', true), '')::uuid
$$;

CREATE OR REPLACE FUNCTION platform.current_user_id()
RETURNS uuid
LANGUAGE sql STABLE AS $$
  SELECT NULLIF(current_setting('app.user_id', true), '')::uuid
$$;

CREATE OR REPLACE FUNCTION platform.current_store_id()
RETURNS uuid
LANGUAGE sql STABLE AS $$
  SELECT NULLIF(current_setting('app.store_id', true), '')::uuid
$$;

CREATE OR REPLACE FUNCTION platform.set_updated_at()
RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  NEW.updated_at = clock_timestamp();
  RETURN NEW;
END $$;

-- ============================================================
-- 2. ORGANIZATION
-- ============================================================
CREATE TABLE organization.company (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  code varchar(30) NOT NULL,
  name varchar(150) NOT NULL,
  legal_name varchar(200),
  tax_id varchar(100),
  base_currency char(3) NOT NULL DEFAULT 'IDR',
  timezone varchar(64) NOT NULL DEFAULT 'Asia/Jakarta',
  status varchar(20) NOT NULL DEFAULT 'ACTIVE'
    CHECK (status IN ('ACTIVE','INACTIVE')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid,
  updated_by uuid,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  UNIQUE (tenant_id, code)
);

CREATE TABLE organization.region (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  company_id uuid NOT NULL REFERENCES organization.company(id),
  code varchar(30) NOT NULL,
  name varchar(150) NOT NULL,
  status varchar(20) NOT NULL DEFAULT 'ACTIVE'
    CHECK (status IN ('ACTIVE','INACTIVE')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid,
  updated_by uuid,
  version bigint NOT NULL DEFAULT 1,
  UNIQUE (company_id, code)
);

CREATE TABLE organization.store (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  company_id uuid NOT NULL REFERENCES organization.company(id),
  region_id uuid REFERENCES organization.region(id),
  code varchar(30) NOT NULL,
  name varchar(150) NOT NULL,
  address text,
  timezone varchar(64) NOT NULL DEFAULT 'Asia/Jakarta',
  status varchar(20) NOT NULL DEFAULT 'ACTIVE'
    CHECK (status IN ('ACTIVE','INACTIVE')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid,
  updated_by uuid,
  version bigint NOT NULL DEFAULT 1,
  UNIQUE (company_id, code)
);

CREATE TABLE organization.warehouse (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  company_id uuid NOT NULL REFERENCES organization.company(id),
  store_id uuid REFERENCES organization.store(id),
  code varchar(30) NOT NULL,
  name varchar(150) NOT NULL,
  warehouse_type varchar(30) NOT NULL DEFAULT 'STORE'
    CHECK (warehouse_type IN ('STORE','CENTRAL','TRANSIT','VIRTUAL','SERVICE')),
  status varchar(20) NOT NULL DEFAULT 'ACTIVE'
    CHECK (status IN ('ACTIVE','INACTIVE')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid,
  updated_by uuid,
  version bigint NOT NULL DEFAULT 1,
  UNIQUE (company_id, code)
);

CREATE TABLE organization.terminal (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  company_id uuid NOT NULL REFERENCES organization.company(id),
  store_id uuid NOT NULL REFERENCES organization.store(id),
  code varchar(30) NOT NULL,
  name varchar(100) NOT NULL,
  device_identifier varchar(150),
  status varchar(20) NOT NULL DEFAULT 'ACTIVE'
    CHECK (status IN ('ACTIVE','INACTIVE','MAINTENANCE')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid,
  updated_by uuid,
  version bigint NOT NULL DEFAULT 1,
  UNIQUE (store_id, code),
  UNIQUE (device_identifier)
);

CREATE INDEX ix_store_company ON organization.store(company_id);
CREATE INDEX ix_terminal_store ON organization.terminal(store_id);
CREATE INDEX ix_warehouse_company ON organization.warehouse(company_id);

-- ============================================================
-- 3. IDENTITY / RBAC
-- ============================================================
CREATE TABLE identity.user_account (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  username citext NOT NULL,
  email citext,
  password_hash text,
  display_name varchar(150) NOT NULL,
  status varchar(20) NOT NULL DEFAULT 'ACTIVE'
    CHECK (status IN ('ACTIVE','LOCKED','DISABLED','PENDING')),
  failed_login_count integer NOT NULL DEFAULT 0 CHECK (failed_login_count >= 0),
  locked_until timestamptz,
  last_login_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid,
  updated_by uuid,
  version bigint NOT NULL DEFAULT 1,
  UNIQUE (tenant_id, username),
  UNIQUE (tenant_id, email)
);

CREATE TABLE identity.role (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  code varchar(50) NOT NULL,
  name varchar(100) NOT NULL,
  description text,
  is_system boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid,
  updated_by uuid,
  version bigint NOT NULL DEFAULT 1,
  UNIQUE (tenant_id, code)
);

CREATE TABLE identity.permission (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  code varchar(100) NOT NULL UNIQUE,
  name varchar(150) NOT NULL,
  module varchar(50) NOT NULL,
  action varchar(50) NOT NULL,
  UNIQUE (module, action)
);

CREATE TABLE identity.user_role (
  user_id uuid NOT NULL REFERENCES identity.user_account(id),
  role_id uuid NOT NULL REFERENCES identity.role(id),
  assigned_at timestamptz NOT NULL DEFAULT now(),
  assigned_by uuid,
  PRIMARY KEY (user_id, role_id)
);

CREATE TABLE identity.role_permission (
  role_id uuid NOT NULL REFERENCES identity.role(id),
  permission_id uuid NOT NULL REFERENCES identity.permission(id),
  PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE identity.user_scope (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  user_id uuid NOT NULL REFERENCES identity.user_account(id),
  company_id uuid REFERENCES organization.company(id),
  region_id uuid REFERENCES organization.region(id),
  store_id uuid REFERENCES organization.store(id),
  warehouse_id uuid REFERENCES organization.warehouse(id),
  terminal_id uuid REFERENCES organization.terminal(id),
  scope_type varchar(20) NOT NULL
    CHECK (scope_type IN ('COMPANY','REGION','STORE','WAREHOUSE','TERMINAL')),
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE identity.session (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  user_id uuid NOT NULL REFERENCES identity.user_account(id),
  token_hash text NOT NULL UNIQUE,
  ip_address inet,
  user_agent text,
  issued_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  revoked_at timestamptz,
  last_seen_at timestamptz,
  CHECK (expires_at > issued_at)
);

CREATE TABLE identity.mfa_factor (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  user_id uuid NOT NULL REFERENCES identity.user_account(id),
  factor_type varchar(30) NOT NULL
    CHECK (factor_type IN ('TOTP','WEBAUTHN','PASSKEY','RECOVERY_CODE')),
  secret_encrypted text,
  credential_id text,
  enabled boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE identity.login_attempt (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id uuid,
  user_id uuid REFERENCES identity.user_account(id),
  username citext,
  success boolean NOT NULL,
  failure_reason varchar(100),
  ip_address inet,
  user_agent text,
  occurred_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX ix_user_scope_user ON identity.user_scope(user_id);
CREATE INDEX ix_session_user ON identity.session(user_id);
CREATE INDEX ix_login_attempt_time ON identity.login_attempt(occurred_at DESC);

-- ============================================================
-- 4. MASTER DATA
-- ============================================================
CREATE TABLE master.brand (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  code varchar(50) NOT NULL,
  name varchar(150) NOT NULL,
  status varchar(20) NOT NULL DEFAULT 'ACTIVE'
    CHECK (status IN ('ACTIVE','INACTIVE')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, code)
);

CREATE TABLE master.category (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  parent_id uuid REFERENCES master.category(id),
  code varchar(50) NOT NULL,
  name varchar(150) NOT NULL,
  status varchar(20) NOT NULL DEFAULT 'ACTIVE'
    CHECK (status IN ('ACTIVE','INACTIVE')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, code)
);

CREATE TABLE master.uom (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  code varchar(30) NOT NULL,
  name varchar(80) NOT NULL,
  precision_scale smallint NOT NULL DEFAULT 0 CHECK (precision_scale BETWEEN 0 AND 6),
  UNIQUE (tenant_id, code)
);

CREATE TABLE master.product (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  company_id uuid REFERENCES organization.company(id),
  sku varchar(80) NOT NULL,
  name varchar(200) NOT NULL,
  brand_id uuid REFERENCES master.brand(id),
  category_id uuid REFERENCES master.category(id),
  base_uom_id uuid NOT NULL REFERENCES master.uom(id),
  product_type varchar(30) NOT NULL DEFAULT 'STOCKABLE'
    CHECK (product_type IN ('STOCKABLE','SERVICE','CONSUMABLE','BUNDLE')),
  track_serial boolean NOT NULL DEFAULT false,
  track_lot boolean NOT NULL DEFAULT false,
  tax_category varchar(50),
  status varchar(20) NOT NULL DEFAULT 'ACTIVE'
    CHECK (status IN ('ACTIVE','INACTIVE','DISCONTINUED')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid,
  updated_by uuid,
  version bigint NOT NULL DEFAULT 1,
  UNIQUE (tenant_id, sku)
);

CREATE TABLE master.product_variant (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  product_id uuid NOT NULL REFERENCES master.product(id),
  sku varchar(80) NOT NULL,
  name varchar(200) NOT NULL,
  attributes jsonb NOT NULL DEFAULT '{}'::jsonb,
  status varchar(20) NOT NULL DEFAULT 'ACTIVE'
    CHECK (status IN ('ACTIVE','INACTIVE')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, sku)
);

CREATE TABLE master.product_barcode (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  product_id uuid NOT NULL REFERENCES master.product(id),
  variant_id uuid REFERENCES master.product_variant(id),
  barcode varchar(100) NOT NULL,
  barcode_type varchar(30) NOT NULL DEFAULT 'EAN',
  is_primary boolean NOT NULL DEFAULT false,
  status varchar(20) NOT NULL DEFAULT 'ACTIVE',
  UNIQUE (tenant_id, barcode)
);

CREATE TABLE master.serial_number (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  product_id uuid NOT NULL REFERENCES master.product(id),
  variant_id uuid REFERENCES master.product_variant(id),
  serial_no varchar(150) NOT NULL,
  status varchar(30) NOT NULL DEFAULT 'RECEIVED'
    CHECK (status IN ('RECEIVED','AVAILABLE','RESERVED','SOLD','RETURNED','DAMAGED','SERVICE','SCRAPPED')),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, serial_no)
);

CREATE INDEX ix_product_name ON master.product USING gin (to_tsvector('simple', name));
CREATE INDEX ix_product_category ON master.product(category_id);
CREATE INDEX ix_barcode_product ON master.product_barcode(product_id);
CREATE INDEX ix_serial_product ON master.serial_number(product_id);

-- ============================================================
-- 5. CRM
-- ============================================================
CREATE TABLE crm.customer_group (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  code varchar(50) NOT NULL,
  name varchar(100) NOT NULL,
  UNIQUE (tenant_id, code)
);

CREATE TABLE crm.customer (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  customer_no varchar(50) NOT NULL,
  customer_group_id uuid REFERENCES crm.customer_group(id),
  name varchar(200) NOT NULL,
  email citext,
  phone varchar(50),
  tax_id text,
  status varchar(20) NOT NULL DEFAULT 'ACTIVE'
    CHECK (status IN ('ACTIVE','INACTIVE')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid,
  updated_by uuid,
  version bigint NOT NULL DEFAULT 1,
  UNIQUE (tenant_id, customer_no)
);

CREATE TABLE crm.customer_address (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  customer_id uuid NOT NULL REFERENCES crm.customer(id),
  address_type varchar(20) NOT NULL
    CHECK (address_type IN ('BILLING','SHIPPING','OTHER')),
  recipient_name varchar(150),
  phone varchar(50),
  address_line text NOT NULL,
  city varchar(100),
  province varchar(100),
  postal_code varchar(20),
  country_code char(2) NOT NULL DEFAULT 'ID',
  is_default boolean NOT NULL DEFAULT false
);

CREATE INDEX ix_customer_email ON crm.customer(tenant_id, email);
CREATE INDEX ix_customer_phone ON crm.customer(tenant_id, phone);

-- ============================================================
-- 6. PRICING
-- ============================================================
CREATE TABLE pricing.price_list (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  company_id uuid REFERENCES organization.company(id),
  code varchar(50) NOT NULL,
  name varchar(150) NOT NULL,
  currency char(3) NOT NULL DEFAULT 'IDR',
  channel varchar(30) NOT NULL DEFAULT 'ALL',
  status varchar(20) NOT NULL DEFAULT 'ACTIVE',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, code)
);

CREATE TABLE pricing.price_rule (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  price_list_id uuid NOT NULL REFERENCES pricing.price_list(id),
  product_id uuid NOT NULL REFERENCES master.product(id),
  customer_group_id uuid REFERENCES crm.customer_group(id),
  store_id uuid REFERENCES organization.store(id),
  min_qty numeric(18,6) NOT NULL DEFAULT 1 CHECK (min_qty > 0),
  unit_price numeric(19,4) NOT NULL CHECK (unit_price >= 0),
  priority integer NOT NULL DEFAULT 100,
  effective_from timestamptz NOT NULL,
  effective_to timestamptz,
  status varchar(20) NOT NULL DEFAULT 'ACTIVE',
  CHECK (effective_to IS NULL OR effective_to > effective_from)
);

CREATE INDEX ix_price_rule_lookup
ON pricing.price_rule(tenant_id, product_id, price_list_id, status, effective_from, effective_to);

-- ============================================================
-- 7. PROMOTION
-- ============================================================
CREATE TABLE promotion.promotion (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  code varchar(50) NOT NULL,
  name varchar(200) NOT NULL,
  description text,
  channel varchar(30) NOT NULL DEFAULT 'ALL',
  stacking_mode varchar(20) NOT NULL DEFAULT 'STACKABLE'
    CHECK (stacking_mode IN ('STACKABLE','EXCLUSIVE','BEST_ONLY')),
  priority integer NOT NULL DEFAULT 100,
  effective_from timestamptz NOT NULL,
  effective_to timestamptz,
  status varchar(20) NOT NULL DEFAULT 'DRAFT'
    CHECK (status IN ('DRAFT','PENDING_APPROVAL','APPROVED','ACTIVE','INACTIVE','EXPIRED')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid,
  updated_by uuid,
  version bigint NOT NULL DEFAULT 1,
  UNIQUE (tenant_id, code),
  CHECK (effective_to IS NULL OR effective_to > effective_from)
);

CREATE TABLE promotion.promotion_condition (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  promotion_id uuid NOT NULL REFERENCES promotion.promotion(id),
  condition_type varchar(50) NOT NULL,
  condition_config jsonb NOT NULL DEFAULT '{}'::jsonb,
  sequence_no integer NOT NULL DEFAULT 1
);

CREATE TABLE promotion.promotion_reward (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  promotion_id uuid NOT NULL REFERENCES promotion.promotion(id),
  reward_type varchar(50) NOT NULL,
  reward_config jsonb NOT NULL DEFAULT '{}'::jsonb,
  sequence_no integer NOT NULL DEFAULT 1
);

CREATE TABLE promotion.promotion_application (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  promotion_id uuid NOT NULL REFERENCES promotion.promotion(id),
  sales_order_id uuid,
  promotion_snapshot jsonb NOT NULL,
  applied_amount numeric(19,4) NOT NULL DEFAULT 0 CHECK (applied_amount >= 0),
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX ix_promotion_active
ON promotion.promotion(tenant_id, status, effective_from, effective_to, priority);

-- ============================================================
-- 8. SALES
-- ============================================================
CREATE SEQUENCE sales.sales_order_seq AS bigint START 1;

CREATE TABLE sales.sales_order (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  company_id uuid NOT NULL REFERENCES organization.company(id),
  store_id uuid REFERENCES organization.store(id),
  terminal_id uuid REFERENCES organization.terminal(id),
  customer_id uuid REFERENCES crm.customer(id),
  source varchar(30) NOT NULL DEFAULT 'POS',
  order_number varchar(80) NOT NULL,
  order_date timestamptz NOT NULL DEFAULT now(),
  currency char(3) NOT NULL DEFAULT 'IDR',
  subtotal numeric(19,4) NOT NULL DEFAULT 0,
  discount_amount numeric(19,4) NOT NULL DEFAULT 0,
  tax_amount numeric(19,4) NOT NULL DEFAULT 0,
  shipping_amount numeric(19,4) NOT NULL DEFAULT 0,
  grand_total numeric(19,4) NOT NULL DEFAULT 0,
  paid_amount numeric(19,4) NOT NULL DEFAULT 0,
  status varchar(30) NOT NULL DEFAULT 'DRAFT'
    CHECK (status IN ('DRAFT','CONFIRMED','PAID','COMPLETED','CANCELLED','VOIDED','RETURNED','PARTIALLY_REFUNDED','REFUNDED')),
  source_reference varchar(150),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid,
  updated_by uuid,
  version bigint NOT NULL DEFAULT 1,
  UNIQUE (tenant_id, order_number),
  CHECK (subtotal >= 0 AND discount_amount >= 0 AND tax_amount >= 0 AND shipping_amount >= 0 AND grand_total >= 0 AND paid_amount >= 0)
);

CREATE TABLE sales.sales_order_item (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  sales_order_id uuid NOT NULL REFERENCES sales.sales_order(id),
  product_id uuid NOT NULL REFERENCES master.product(id),
  variant_id uuid REFERENCES master.product_variant(id),
  sku_snapshot varchar(80) NOT NULL,
  product_name_snapshot varchar(200) NOT NULL,
  qty numeric(18,6) NOT NULL CHECK (qty > 0),
  unit_price numeric(19,4) NOT NULL CHECK (unit_price >= 0),
  gross_amount numeric(19,4) NOT NULL CHECK (gross_amount >= 0),
  discount_amount numeric(19,4) NOT NULL DEFAULT 0 CHECK (discount_amount >= 0),
  tax_amount numeric(19,4) NOT NULL DEFAULT 0 CHECK (tax_amount >= 0),
  net_amount numeric(19,4) NOT NULL CHECK (net_amount >= 0),
  cost_snapshot numeric(19,4),
  serial_numbers jsonb NOT NULL DEFAULT '[]'::jsonb
);

CREATE INDEX ix_sales_order_store_date ON sales.sales_order(store_id, order_date DESC);
CREATE INDEX ix_sales_order_customer ON sales.sales_order(customer_id, order_date DESC);
CREATE INDEX ix_sales_order_status ON sales.sales_order(tenant_id, status, order_date DESC);
CREATE INDEX ix_sales_item_product ON sales.sales_order_item(product_id);

-- ============================================================
-- 9. POS
-- ============================================================
CREATE TABLE pos.session (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  company_id uuid NOT NULL REFERENCES organization.company(id),
  store_id uuid NOT NULL REFERENCES organization.store(id),
  terminal_id uuid NOT NULL REFERENCES organization.terminal(id),
  cashier_user_id uuid NOT NULL REFERENCES identity.user_account(id),
  session_number varchar(80) NOT NULL,
  opened_at timestamptz NOT NULL DEFAULT now(),
  closed_at timestamptz,
  opening_cash numeric(19,4) NOT NULL DEFAULT 0 CHECK (opening_cash >= 0),
  expected_cash numeric(19,4) NOT NULL DEFAULT 0,
  actual_cash numeric(19,4),
  variance numeric(19,4),
  status varchar(20) NOT NULL DEFAULT 'OPEN'
    CHECK (status IN ('OPEN','ACTIVE','CLOSING','CLOSED')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  version bigint NOT NULL DEFAULT 1,
  UNIQUE (tenant_id, session_number)
);

-- Only one non-closed POS session may exist per terminal.
CREATE UNIQUE INDEX ux_pos_session_terminal_open
ON pos.session(terminal_id)
WHERE status IN ('OPEN','ACTIVE','CLOSING');

CREATE TABLE pos.cash_movement (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  session_id uuid NOT NULL REFERENCES pos.session(id),
  movement_type varchar(30) NOT NULL
    CHECK (movement_type IN ('CASH_IN','CASH_OUT','SALE','REFUND','OPENING','CLOSING','ADJUSTMENT')),
  amount numeric(19,4) NOT NULL CHECK (amount > 0),
  reason varchar(255),
  reference_type varchar(50),
  reference_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid
);

CREATE INDEX ix_pos_session_store ON pos.session(store_id, opened_at DESC);
CREATE INDEX ix_cash_movement_session ON pos.cash_movement(session_id, created_at);

-- ============================================================
-- 10. PAYMENT
-- ============================================================
CREATE TABLE payment.provider (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  code varchar(50) NOT NULL,
  name varchar(100) NOT NULL,
  provider_type varchar(30) NOT NULL,
  config_encrypted text,
  status varchar(20) NOT NULL DEFAULT 'ACTIVE',
  UNIQUE (tenant_id, code)
);

CREATE TABLE payment.payment (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  company_id uuid NOT NULL REFERENCES organization.company(id),
  sales_order_id uuid NOT NULL REFERENCES sales.sales_order(id),
  provider_id uuid REFERENCES payment.provider(id),
  payment_method varchar(30) NOT NULL,
  amount numeric(19,4) NOT NULL CHECK (amount > 0),
  currency char(3) NOT NULL DEFAULT 'IDR',
  status varchar(30) NOT NULL DEFAULT 'PENDING'
    CHECK (status IN ('PENDING','PROCESSING','SUCCESS','FAILED','CANCELLED','REFUNDED','REVERSED')),
  provider_reference varchar(200),
  idempotency_key varchar(200) NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  version bigint NOT NULL DEFAULT 1,
  UNIQUE (tenant_id, idempotency_key)
);

CREATE TABLE payment.payment_attempt (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  payment_id uuid NOT NULL REFERENCES payment.payment(id),
  attempt_no integer NOT NULL CHECK (attempt_no > 0),
  status varchar(30) NOT NULL,
  request_reference varchar(200),
  provider_reference varchar(200),
  response_code varchar(100),
  response_message text,
  attempted_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (payment_id, attempt_no)
);

CREATE TABLE payment.refund (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  payment_id uuid NOT NULL REFERENCES payment.payment(id),
  sales_order_id uuid NOT NULL REFERENCES sales.sales_order(id),
  amount numeric(19,4) NOT NULL CHECK (amount > 0),
  reason text,
  status varchar(20) NOT NULL DEFAULT 'PENDING'
    CHECK (status IN ('PENDING','PROCESSING','SUCCESS','FAILED','CANCELLED')),
  provider_reference varchar(200),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid
);

CREATE INDEX ix_payment_order ON payment.payment(sales_order_id);
CREATE INDEX ix_payment_status ON payment.payment(tenant_id, status, created_at DESC);
CREATE INDEX ix_payment_provider_ref ON payment.payment(provider_reference);

-- ============================================================
-- 11. WAREHOUSE / INVENTORY
-- ============================================================
CREATE TABLE warehouse.location (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  warehouse_id uuid NOT NULL REFERENCES organization.warehouse(id),
  parent_id uuid REFERENCES warehouse.location(id),
  code varchar(50) NOT NULL,
  name varchar(100) NOT NULL,
  location_type varchar(30) NOT NULL DEFAULT 'STORAGE',
  status varchar(20) NOT NULL DEFAULT 'ACTIVE',
  UNIQUE (warehouse_id, code)
);

CREATE TABLE inventory.stock (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  warehouse_id uuid NOT NULL REFERENCES organization.warehouse(id),
  location_id uuid NOT NULL REFERENCES warehouse.location(id),
  product_id uuid NOT NULL REFERENCES master.product(id),
  variant_id uuid REFERENCES master.product_variant(id),
  on_hand_qty numeric(18,6) NOT NULL DEFAULT 0 CHECK (on_hand_qty >= 0),
  reserved_qty numeric(18,6) NOT NULL DEFAULT 0 CHECK (reserved_qty >= 0),
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  UNIQUE (warehouse_id, location_id, product_id, variant_id),
  CHECK (reserved_qty <= on_hand_qty)
);

CREATE TABLE inventory.stock_ledger (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  warehouse_id uuid NOT NULL REFERENCES organization.warehouse(id),
  location_id uuid NOT NULL REFERENCES warehouse.location(id),
  product_id uuid NOT NULL REFERENCES master.product(id),
  variant_id uuid REFERENCES master.product_variant(id),
  movement_type varchar(30) NOT NULL
    CHECK (movement_type IN ('PURCHASE_RECEIPT','SALE','RETURN','TRANSFER_IN','TRANSFER_OUT','ADJUSTMENT','OPNAME','DAMAGE','SERVICE')),
  qty_delta numeric(18,6) NOT NULL CHECK (qty_delta <> 0),
  reference_type varchar(50),
  reference_id uuid,
  balance_after numeric(18,6),
  occurred_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid
);

CREATE TABLE inventory.stock_reservation (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  warehouse_id uuid NOT NULL REFERENCES organization.warehouse(id),
  location_id uuid NOT NULL REFERENCES warehouse.location(id),
  product_id uuid NOT NULL REFERENCES master.product(id),
  variant_id uuid REFERENCES master.product_variant(id),
  reference_type varchar(50) NOT NULL,
  reference_id uuid NOT NULL,
  qty numeric(18,6) NOT NULL CHECK (qty > 0),
  status varchar(20) NOT NULL DEFAULT 'RESERVED'
    CHECK (status IN ('RESERVED','RELEASED','CONSUMED','EXPIRED')),
  expires_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE inventory.stock_transfer (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  company_id uuid NOT NULL REFERENCES organization.company(id),
  transfer_number varchar(80) NOT NULL,
  from_warehouse_id uuid NOT NULL REFERENCES organization.warehouse(id),
  to_warehouse_id uuid NOT NULL REFERENCES organization.warehouse(id),
  status varchar(20) NOT NULL DEFAULT 'DRAFT'
    CHECK (status IN ('DRAFT','REQUESTED','APPROVED','IN_TRANSIT','RECEIVED','CANCELLED')),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid,
  UNIQUE (tenant_id, transfer_number),
  CHECK (from_warehouse_id <> to_warehouse_id)
);

CREATE TABLE inventory.stock_adjustment (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  company_id uuid NOT NULL REFERENCES organization.company(id),
  warehouse_id uuid NOT NULL REFERENCES organization.warehouse(id),
  adjustment_number varchar(80) NOT NULL,
  reason text NOT NULL,
  status varchar(20) NOT NULL DEFAULT 'DRAFT'
    CHECK (status IN ('DRAFT','PENDING_APPROVAL','APPROVED','POSTED','CANCELLED')),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid,
  UNIQUE (tenant_id, adjustment_number)
);

CREATE INDEX ix_stock_lookup ON inventory.stock(warehouse_id, product_id);
CREATE INDEX ix_stock_ledger_product_time ON inventory.stock_ledger(product_id, occurred_at DESC);
CREATE INDEX ix_reservation_reference ON inventory.stock_reservation(reference_type, reference_id);

-- ============================================================
-- 12. PURCHASING
-- ============================================================
CREATE TABLE purchasing.supplier (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  supplier_no varchar(50) NOT NULL,
  name varchar(200) NOT NULL,
  tax_id text,
  status varchar(20) NOT NULL DEFAULT 'ACTIVE',
  UNIQUE (tenant_id, supplier_no)
);

CREATE TABLE purchasing.purchase_order (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  company_id uuid NOT NULL REFERENCES organization.company(id),
  supplier_id uuid NOT NULL REFERENCES purchasing.supplier(id),
  po_number varchar(80) NOT NULL,
  order_date timestamptz NOT NULL DEFAULT now(),
  status varchar(30) NOT NULL DEFAULT 'DRAFT'
    CHECK (status IN ('DRAFT','SUBMITTED','APPROVED','PARTIALLY_RECEIVED','RECEIVED','CANCELLED')),
  total_amount numeric(19,4) NOT NULL DEFAULT 0 CHECK (total_amount >= 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid,
  UNIQUE (tenant_id, po_number)
);

CREATE TABLE purchasing.goods_receipt (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  company_id uuid NOT NULL REFERENCES organization.company(id),
  supplier_id uuid REFERENCES purchasing.supplier(id),
  purchase_order_id uuid REFERENCES purchasing.purchase_order(id),
  receipt_number varchar(80) NOT NULL,
  warehouse_id uuid NOT NULL REFERENCES organization.warehouse(id),
  received_at timestamptz NOT NULL DEFAULT now(),
  status varchar(20) NOT NULL DEFAULT 'POSTED'
    CHECK (status IN ('DRAFT','POSTED','CANCELLED')),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid,
  UNIQUE (tenant_id, receipt_number)
);

-- ============================================================
-- 13. FINANCE
-- ============================================================
CREATE TABLE finance.tax (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  code varchar(50) NOT NULL,
  name varchar(100) NOT NULL,
  rate numeric(8,4) NOT NULL CHECK (rate >= 0),
  effective_from date NOT NULL,
  effective_to date,
  status varchar(20) NOT NULL DEFAULT 'ACTIVE',
  UNIQUE (tenant_id, code),
  CHECK (effective_to IS NULL OR effective_to >= effective_from)
);

CREATE TABLE finance.accounting_mapping (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  company_id uuid REFERENCES organization.company(id),
  mapping_type varchar(50) NOT NULL,
  business_code varchar(100) NOT NULL,
  external_account_code varchar(100) NOT NULL,
  external_system varchar(50) NOT NULL,
  status varchar(20) NOT NULL DEFAULT 'ACTIVE',
  UNIQUE (tenant_id, mapping_type, business_code, external_system)
);

CREATE TABLE finance.settlement (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  company_id uuid NOT NULL REFERENCES organization.company(id),
  provider_id uuid REFERENCES payment.provider(id),
  settlement_reference varchar(200) NOT NULL,
  settlement_date date NOT NULL,
  gross_amount numeric(19,4) NOT NULL DEFAULT 0,
  fee_amount numeric(19,4) NOT NULL DEFAULT 0,
  tax_amount numeric(19,4) NOT NULL DEFAULT 0,
  net_amount numeric(19,4) NOT NULL DEFAULT 0,
  status varchar(20) NOT NULL DEFAULT 'PENDING',
  UNIQUE (tenant_id, settlement_reference)
);

-- ============================================================
-- 14. LOYALTY
-- ============================================================
CREATE TABLE loyalty.account (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  customer_id uuid NOT NULL REFERENCES crm.customer(id),
  account_no varchar(80) NOT NULL,
  balance numeric(19,4) NOT NULL DEFAULT 0 CHECK (balance >= 0),
  status varchar(20) NOT NULL DEFAULT 'ACTIVE',
  UNIQUE (tenant_id, account_no),
  UNIQUE (customer_id)
);

CREATE TABLE loyalty.transaction (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  account_id uuid NOT NULL REFERENCES loyalty.account(id),
  transaction_type varchar(30) NOT NULL
    CHECK (transaction_type IN ('EARN','REDEEM','EXPIRE','ADJUST','REFUND_REVERSAL')),
  amount numeric(19,4) NOT NULL CHECK (amount > 0),
  reference_type varchar(50),
  reference_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid
);

CREATE TABLE loyalty.voucher (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  code varchar(100) NOT NULL,
  voucher_type varchar(30) NOT NULL,
  value numeric(19,4) NOT NULL DEFAULT 0,
  max_redemption integer,
  redeemed_count integer NOT NULL DEFAULT 0,
  effective_from timestamptz,
  effective_to timestamptz,
  status varchar(20) NOT NULL DEFAULT 'ACTIVE',
  UNIQUE (tenant_id, code)
);

CREATE TABLE loyalty.voucher_redemption (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  voucher_id uuid NOT NULL REFERENCES loyalty.voucher(id),
  customer_id uuid REFERENCES crm.customer(id),
  sales_order_id uuid REFERENCES sales.sales_order(id),
  redeemed_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (voucher_id, sales_order_id)
);

-- ============================================================
-- 15. WORKFLOW
-- ============================================================
CREATE TABLE workflow.definition (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  code varchar(80) NOT NULL,
  name varchar(150) NOT NULL,
  resource_type varchar(100) NOT NULL,
  status varchar(20) NOT NULL DEFAULT 'ACTIVE',
  UNIQUE (tenant_id, code)
);

CREATE TABLE workflow.state (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workflow_id uuid NOT NULL REFERENCES workflow.definition(id),
  code varchar(50) NOT NULL,
  name varchar(100) NOT NULL,
  is_initial boolean NOT NULL DEFAULT false,
  is_final boolean NOT NULL DEFAULT false,
  UNIQUE (workflow_id, code)
);

CREATE TABLE workflow.transition (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workflow_id uuid NOT NULL REFERENCES workflow.definition(id),
  from_state_id uuid NOT NULL REFERENCES workflow.state(id),
  to_state_id uuid NOT NULL REFERENCES workflow.state(id),
  action_code varchar(80) NOT NULL,
  required_permission varchar(100),
  UNIQUE (workflow_id, from_state_id, action_code)
);

CREATE TABLE workflow.instance (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  workflow_id uuid NOT NULL REFERENCES workflow.definition(id),
  resource_type varchar(100) NOT NULL,
  resource_id uuid NOT NULL,
  current_state_id uuid NOT NULL REFERENCES workflow.state(id),
  started_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz,
  UNIQUE (workflow_id, resource_type, resource_id)
);

CREATE TABLE workflow.approval (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  workflow_instance_id uuid NOT NULL REFERENCES workflow.instance(id),
  approver_user_id uuid NOT NULL REFERENCES identity.user_account(id),
  decision varchar(20) NOT NULL CHECK (decision IN ('APPROVED','REJECTED','PENDING')),
  remarks text,
  decided_at timestamptz
);

-- ============================================================
-- 16. INTEGRATION / OUTBOX / IDEMPOTENCY
-- ============================================================
CREATE TABLE integration.outbox_event (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  aggregate_type varchar(100) NOT NULL,
  aggregate_id uuid NOT NULL,
  event_type varchar(150) NOT NULL,
  event_version integer NOT NULL DEFAULT 1,
  payload jsonb NOT NULL,
  status varchar(20) NOT NULL DEFAULT 'PENDING'
    CHECK (status IN ('PENDING','PUBLISHED','FAILED','DEAD_LETTER')),
  occurred_at timestamptz NOT NULL DEFAULT now(),
  published_at timestamptz,
  retry_count integer NOT NULL DEFAULT 0,
  last_error text,
  UNIQUE (id)
);

CREATE TABLE integration.delivery_attempt (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  event_id uuid NOT NULL REFERENCES integration.outbox_event(id),
  target_system varchar(100) NOT NULL,
  attempt_no integer NOT NULL,
  status varchar(20) NOT NULL,
  response_code varchar(50),
  response_body text,
  attempted_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (event_id, target_system, attempt_no)
);

CREATE TABLE integration.external_reference (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  internal_type varchar(100) NOT NULL,
  internal_id uuid NOT NULL,
  external_system varchar(100) NOT NULL,
  external_type varchar(100),
  external_id varchar(200) NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, internal_type, internal_id, external_system),
  UNIQUE (external_system, external_type, external_id)
);

CREATE TABLE integration.webhook_event (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid,
  provider_code varchar(100) NOT NULL,
  event_id varchar(200) NOT NULL,
  event_type varchar(150),
  signature_valid boolean NOT NULL DEFAULT false,
  payload jsonb NOT NULL,
  received_at timestamptz NOT NULL DEFAULT now(),
  processed_at timestamptz,
  status varchar(20) NOT NULL DEFAULT 'RECEIVED',
  UNIQUE (provider_code, event_id)
);

CREATE TABLE platform.idempotency_key (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  key varchar(200) NOT NULL,
  operation varchar(150) NOT NULL,
  request_hash varchar(128) NOT NULL,
  response_status integer,
  response_body jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz,
  UNIQUE (tenant_id, key, operation)
);

CREATE INDEX ix_outbox_pending ON integration.outbox_event(status, occurred_at);
CREATE INDEX ix_outbox_aggregate ON integration.outbox_event(aggregate_type, aggregate_id);
CREATE INDEX ix_idempotency_expiry ON platform.idempotency_key(expires_at);

-- ============================================================
-- 17. AUDIT / SECURITY LOG
-- ============================================================
CREATE TABLE audit.audit_log (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id uuid NOT NULL,
  company_id uuid,
  store_id uuid,
  terminal_id uuid,
  user_id uuid,
  module varchar(50) NOT NULL,
  action varchar(100) NOT NULL,
  resource_type varchar(100),
  resource_id uuid,
  business_number varchar(100),
  status varchar(20) NOT NULL DEFAULT 'SUCCESS'
    CHECK (status IN ('SUCCESS','FAILED','DENIED')),
  request_id uuid,
  trace_id varchar(100),
  ip_address inet,
  user_agent text,
  reason text,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE audit.data_change (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  audit_log_id bigint NOT NULL REFERENCES audit.audit_log(id),
  table_schema varchar(100) NOT NULL,
  table_name varchar(100) NOT NULL,
  row_id uuid,
  field_name varchar(150) NOT NULL,
  old_value jsonb,
  new_value jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE audit.security_event (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id uuid,
  user_id uuid REFERENCES identity.user_account(id),
  event_type varchar(80) NOT NULL,
  severity varchar(20) NOT NULL DEFAULT 'INFO'
    CHECK (severity IN ('INFO','LOW','MEDIUM','HIGH','CRITICAL')),
  ip_address inet,
  user_agent text,
  request_id uuid,
  trace_id varchar(100),
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  occurred_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE audit.login_history (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id uuid,
  user_id uuid REFERENCES identity.user_account(id),
  username citext,
  success boolean NOT NULL,
  failure_reason varchar(100),
  ip_address inet,
  user_agent text,
  occurred_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX ix_audit_time ON audit.audit_log(created_at DESC);
CREATE INDEX ix_audit_user_time ON audit.audit_log(user_id, created_at DESC);
CREATE INDEX ix_audit_resource ON audit.audit_log(resource_type, resource_id, created_at DESC);
CREATE INDEX ix_audit_module_action ON audit.audit_log(module, action, created_at DESC);
CREATE INDEX ix_audit_trace ON audit.audit_log(trace_id);
CREATE INDEX ix_data_change_audit ON audit.data_change(audit_log_id);
CREATE INDEX ix_security_event_time ON audit.security_event(occurred_at DESC);

-- Audit tables are append-only at application level.
-- Do NOT grant UPDATE/DELETE to the application role.

-- ============================================================
-- 18. UPDATED_AT TRIGGERS
-- ============================================================
DO $$
DECLARE
  t text;
  tables text[] := ARRAY[
    'organization.company','organization.region','organization.store',
    'organization.warehouse','organization.terminal',
    'identity.user_account','identity.role',
    'master.brand','master.category','master.product','master.product_variant',
    'crm.customer','pricing.price_list','promotion.promotion',
    'sales.sales_order','pos.session','payment.payment'
  ];
BEGIN
  FOREACH t IN ARRAY tables LOOP
    EXECUTE format(
      'DROP TRIGGER IF EXISTS trg_updated_at ON %s;
       CREATE TRIGGER trg_updated_at BEFORE UPDATE ON %s
       FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();', t, t
    );
  END LOOP;
END $$;

-- ============================================================
-- 19. IMMUTABILITY GUARDS FOR LEDGERS / AUDIT
-- ============================================================
CREATE OR REPLACE FUNCTION platform.prevent_mutation()
RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'Immutable record: %.%', TG_TABLE_SCHEMA, TG_TABLE_NAME;
END $$;

CREATE TRIGGER trg_stock_ledger_immutable
BEFORE UPDATE OR DELETE ON inventory.stock_ledger
FOR EACH ROW EXECUTE FUNCTION platform.prevent_mutation();

CREATE TRIGGER trg_loyalty_transaction_immutable
BEFORE UPDATE OR DELETE ON loyalty.transaction
FOR EACH ROW EXECUTE FUNCTION platform.prevent_mutation();

CREATE TRIGGER trg_audit_log_immutable
BEFORE UPDATE OR DELETE ON audit.audit_log
FOR EACH ROW EXECUTE FUNCTION platform.prevent_mutation();

CREATE TRIGGER trg_audit_data_change_immutable
BEFORE UPDATE OR DELETE ON audit.data_change
FOR EACH ROW EXECUTE FUNCTION platform.prevent_mutation();

CREATE TRIGGER trg_security_event_immutable
BEFORE UPDATE OR DELETE ON audit.security_event
FOR EACH ROW EXECUTE FUNCTION platform.prevent_mutation();

-- ============================================================
-- 20. RLS
-- ============================================================
ALTER TABLE organization.company ENABLE ROW LEVEL SECURITY;
ALTER TABLE organization.store ENABLE ROW LEVEL SECURITY;
ALTER TABLE organization.warehouse ENABLE ROW LEVEL SECURITY;
ALTER TABLE organization.terminal ENABLE ROW LEVEL SECURITY;
ALTER TABLE master.product ENABLE ROW LEVEL SECURITY;
ALTER TABLE crm.customer ENABLE ROW LEVEL SECURITY;
ALTER TABLE pricing.price_rule ENABLE ROW LEVEL SECURITY;
ALTER TABLE promotion.promotion ENABLE ROW LEVEL SECURITY;
ALTER TABLE sales.sales_order ENABLE ROW LEVEL SECURITY;
ALTER TABLE pos.session ENABLE ROW LEVEL SECURITY;
ALTER TABLE payment.payment ENABLE ROW LEVEL SECURITY;
ALTER TABLE inventory.stock ENABLE ROW LEVEL SECURITY;
ALTER TABLE inventory.stock_ledger ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit.audit_log ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_company_isolation ON organization.company
USING (tenant_id = platform.current_tenant_id())
WITH CHECK (tenant_id = platform.current_tenant_id());

CREATE POLICY tenant_store_isolation ON organization.store
USING (tenant_id = platform.current_tenant_id())
WITH CHECK (tenant_id = platform.current_tenant_id());

CREATE POLICY tenant_warehouse_isolation ON organization.warehouse
USING (tenant_id = platform.current_tenant_id())
WITH CHECK (tenant_id = platform.current_tenant_id());

CREATE POLICY tenant_terminal_isolation ON organization.terminal
USING (tenant_id = platform.current_tenant_id())
WITH CHECK (tenant_id = platform.current_tenant_id());

CREATE POLICY tenant_product_isolation ON master.product
USING (tenant_id = platform.current_tenant_id())
WITH CHECK (tenant_id = platform.current_tenant_id());

CREATE POLICY tenant_customer_isolation ON crm.customer
USING (tenant_id = platform.current_tenant_id())
WITH CHECK (tenant_id = platform.current_tenant_id());

CREATE POLICY tenant_price_rule_isolation ON pricing.price_rule
USING (tenant_id = platform.current_tenant_id())
WITH CHECK (tenant_id = platform.current_tenant_id());

CREATE POLICY tenant_promotion_isolation ON promotion.promotion
USING (tenant_id = platform.current_tenant_id())
WITH CHECK (tenant_id = platform.current_tenant_id());

CREATE POLICY tenant_sales_isolation ON sales.sales_order
USING (tenant_id = platform.current_tenant_id())
WITH CHECK (tenant_id = platform.current_tenant_id());

CREATE POLICY tenant_pos_isolation ON pos.session
USING (tenant_id = platform.current_tenant_id())
WITH CHECK (tenant_id = platform.current_tenant_id());

CREATE POLICY tenant_payment_isolation ON payment.payment
USING (tenant_id = platform.current_tenant_id())
WITH CHECK (tenant_id = platform.current_tenant_id());

CREATE POLICY tenant_stock_isolation ON inventory.stock
USING (tenant_id = platform.current_tenant_id())
WITH CHECK (tenant_id = platform.current_tenant_id());

CREATE POLICY tenant_stock_ledger_isolation ON inventory.stock_ledger
USING (tenant_id = platform.current_tenant_id())
WITH CHECK (tenant_id = platform.current_tenant_id());

CREATE POLICY tenant_audit_isolation ON audit.audit_log
USING (tenant_id = platform.current_tenant_id())
WITH CHECK (tenant_id = platform.current_tenant_id());

-- ============================================================
-- 21. DB ROLES (execute separately in controlled migration)
-- ============================================================
-- Recommended:
-- CREATE ROLE pos_app NOLOGIN;
-- CREATE ROLE pos_worker NOLOGIN;
-- CREATE ROLE pos_reporting NOLOGIN;
-- CREATE ROLE pos_migration NOLOGIN;
-- CREATE ROLE pos_readonly NOLOGIN;
-- CREATE ROLE pos_admin NOLOGIN;
--
-- The application MUST NOT use the postgres superuser.
-- Grant only schema/table privileges required by each role.
-- RLS remains enabled for tenant isolation.

-- ============================================================
-- 22. DOCUMENT NUMBERING
-- ============================================================
-- Business numbers must be separate from UUID primary keys.
-- Recommended pattern:
-- POS-JKT01-20261005-000123
-- PO-JKT01-202610-000001
-- ADJ-JKT01-202610-000001
--
-- For high concurrency, implement a dedicated sequence service/table
-- with row locking or PostgreSQL sequences; never MAX(number)+1.

-- V1.1 HARDENING MIGRATION

-- ============================================================
-- V1.1 HARDENING PATCH
-- ============================================================

CREATE TABLE IF NOT EXISTS platform.document_sequence (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  company_id uuid,
  store_id uuid,
  document_type varchar(50) NOT NULL,
  period_key varchar(20) NOT NULL,
  prefix varchar(50) NOT NULL,
  current_value bigint NOT NULL DEFAULT 0 CHECK (current_value >= 0),
  padding smallint NOT NULL DEFAULT 6 CHECK (padding BETWEEN 1 AND 20),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, company_id, store_id, document_type, period_key)
);

CREATE TABLE IF NOT EXISTS platform.system_setting (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  setting_key varchar(150) NOT NULL,
  setting_value jsonb NOT NULL DEFAULT '{}'::jsonb,
  is_secret boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, setting_key)
);

CREATE TABLE IF NOT EXISTS sales.sales_order_status_history (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id uuid NOT NULL,
  sales_order_id uuid NOT NULL REFERENCES sales.sales_order(id),
  from_status varchar(30),
  to_status varchar(30) NOT NULL,
  reason text,
  request_id varchar(100),
  trace_id varchar(100),
  changed_by uuid,
  changed_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS payment.payment_status_history (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id uuid NOT NULL,
  payment_id uuid NOT NULL REFERENCES payment.payment(id),
  from_status varchar(30),
  to_status varchar(30) NOT NULL,
  reason text,
  provider_event_id varchar(200),
  request_id varchar(100),
  trace_id varchar(100),
  changed_by uuid,
  changed_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS inventory.stock_reservation_history (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id uuid NOT NULL,
  reservation_id uuid NOT NULL REFERENCES inventory.stock_reservation(id),
  from_status varchar(30),
  to_status varchar(30) NOT NULL,
  quantity numeric(18,6),
  reason text,
  changed_by uuid,
  changed_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS workflow.transition_history (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id uuid NOT NULL,
  workflow_instance_id uuid NOT NULL REFERENCES workflow.instance(id),
  from_state_id uuid,
  to_state_id uuid NOT NULL,
  action varchar(100) NOT NULL,
  comment text,
  acted_by uuid,
  acted_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS promotion.promotion_version (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  promotion_id uuid NOT NULL REFERENCES promotion.promotion(id),
  version_no integer NOT NULL CHECK (version_no > 0),
  rule_snapshot jsonb NOT NULL,
  status varchar(20) NOT NULL DEFAULT 'DRAFT'
    CHECK (status IN ('DRAFT','APPROVED','ACTIVE','RETIRED')),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid,
  approved_at timestamptz,
  approved_by uuid,
  UNIQUE (promotion_id, version_no)
);

CREATE TABLE IF NOT EXISTS audit.audit_action_catalog (
  code varchar(100) PRIMARY KEY,
  module varchar(50) NOT NULL,
  description varchar(255) NOT NULL,
  severity varchar(20) NOT NULL DEFAULT 'INFO'
    CHECK (severity IN ('INFO','WARNING','HIGH','CRITICAL')),
  requires_reason boolean NOT NULL DEFAULT false,
  active boolean NOT NULL DEFAULT true
);

INSERT INTO audit.audit_action_catalog(code,module,description,severity,requires_reason) VALUES
('SALE_CREATED','SALES','Sales transaction created','INFO',false),
('SALE_COMPLETED','SALES','Sales transaction completed','INFO',false),
('SALE_VOIDED','SALES','Sales transaction voided','HIGH',true),
('SALE_CANCELLED','SALES','Sales transaction cancelled','WARNING',true),
('SALE_REFUNDED','SALES','Sales transaction refunded','HIGH',true),
('PAYMENT_CREATED','PAYMENT','Payment created','INFO',false),
('PAYMENT_SUCCESS','PAYMENT','Payment completed','INFO',false),
('PAYMENT_FAILED','PAYMENT','Payment failed','WARNING',false),
('PAYMENT_REFUNDED','PAYMENT','Payment refunded','HIGH',true),
('STOCK_ADJUSTED','INVENTORY','Stock adjusted','HIGH',true),
('STOCK_TRANSFERRED','INVENTORY','Stock transferred','INFO',false),
('STOCK_OPNAME_POSTED','INVENTORY','Stock opname posted','HIGH',true),
('PRICE_CREATED','PRICING','Price created','INFO',false),
('PRICE_CHANGED','PRICING','Price changed','HIGH',true),
('PROMOTION_CREATED','PROMOTION','Promotion created','INFO',false),
('PROMOTION_APPROVED','PROMOTION','Promotion approved','HIGH',true),
('PROMOTION_ACTIVATED','PROMOTION','Promotion activated','HIGH',false),
('USER_CREATED','IDENTITY','User created','INFO',false),
('USER_DISABLED','IDENTITY','User disabled','HIGH',true),
('USER_ROLE_CHANGED','IDENTITY','User role changed','HIGH',true),
('LOGIN_SUCCESS','SECURITY','Successful login','INFO',false),
('LOGIN_FAILURE','SECURITY','Failed login','WARNING',false),
('MFA_FAILURE','SECURITY','MFA failure','HIGH',false),
('PERMISSION_DENIED','SECURITY','Permission denied','HIGH',false)
ON CONFLICT (code) DO NOTHING;

ALTER TABLE platform.idempotency_key
  ADD COLUMN IF NOT EXISTS operation varchar(100),
  ADD COLUMN IF NOT EXISTS request_hash varchar(128),
  ADD COLUMN IF NOT EXISTS response_status integer,
  ADD COLUMN IF NOT EXISTS response_body jsonb;

CREATE UNIQUE INDEX IF NOT EXISTS ux_idempotency_tenant_operation_key
ON platform.idempotency_key(tenant_id, operation, key)
WHERE operation IS NOT NULL;

-- Database-level same-tenant ownership guard.
CREATE OR REPLACE FUNCTION platform.enforce_same_tenant_fk()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  col_name text := TG_ARGV[0];
  ref_table text := TG_ARGV[1];
  ref_id uuid;
  ref_tenant uuid;
BEGIN
  EXECUTE format('SELECT ($1).%I', col_name) INTO ref_id USING NEW;
  IF ref_id IS NULL THEN RETURN NEW; END IF;
  EXECUTE format('SELECT tenant_id FROM %s WHERE id = $1', ref_table)
    INTO ref_tenant USING ref_id;
  IF ref_tenant IS NULL THEN
    RAISE EXCEPTION 'Referenced row missing or not tenant-aware: %', ref_table;
  END IF;
  IF NEW.tenant_id IS DISTINCT FROM ref_tenant THEN
    RAISE EXCEPTION 'Cross-tenant reference rejected: % -> %', NEW.tenant_id, ref_tenant;
  END IF;
  RETURN NEW;
END $$;

-- High-risk tenant relationships. Ordinary FKs remain for existence checks.
DO $$
DECLARE r record;
BEGIN
  FOR r IN SELECT * FROM (VALUES
    ('organization.region','company_id','organization.company'),
    ('organization.store','company_id','organization.company'),
    ('organization.store','region_id','organization.region'),
    ('organization.warehouse','company_id','organization.company'),
    ('organization.warehouse','store_id','organization.store'),
    ('organization.terminal','company_id','organization.company'),
    ('organization.terminal','store_id','organization.store'),
    ('master.product','company_id','organization.company'),
    ('master.product','brand_id','master.brand'),
    ('master.product','category_id','master.category'),
    ('master.product','base_uom_id','master.uom'),
    ('master.product_variant','product_id','master.product'),
    ('master.product_barcode','product_id','master.product'),
    ('master.product_barcode','variant_id','master.product_variant'),
    ('master.serial_number','product_id','master.product'),
    ('master.serial_number','variant_id','master.product_variant'),
    ('crm.customer','customer_group_id','crm.customer_group'),
    ('crm.customer_address','customer_id','crm.customer'),
    ('pricing.price_rule','product_id','master.product'),
    ('pricing.price_rule','customer_group_id','crm.customer_group'),
    ('pricing.price_rule','store_id','organization.store'),
    ('promotion.promotion_application','promotion_id','promotion.promotion'),
    ('sales.sales_order','company_id','organization.company'),
    ('sales.sales_order','store_id','organization.store'),
    ('sales.sales_order','terminal_id','organization.terminal'),
    ('sales.sales_order','customer_id','crm.customer'),
    ('pos.session','company_id','organization.company'),
    ('pos.session','store_id','organization.store'),
    ('pos.session','terminal_id','organization.terminal'),
    ('pos.session','cashier_user_id','identity.user_account'),
    ('payment.payment','company_id','organization.company'),
    ('payment.payment','sales_order_id','sales.sales_order'),
    ('payment.payment_attempt','payment_id','payment.payment'),
    ('payment.refund','payment_id','payment.payment'),
    ('payment.refund','sales_order_id','sales.sales_order'),
    ('warehouse.location','warehouse_id','organization.warehouse'),
    ('inventory.stock','warehouse_id','organization.warehouse'),
    ('inventory.stock','location_id','warehouse.location'),
    ('inventory.stock','product_id','master.product'),
    ('inventory.stock','variant_id','master.product_variant'),
    ('inventory.stock_ledger','warehouse_id','organization.warehouse'),
    ('inventory.stock_ledger','location_id','warehouse.location'),
    ('inventory.stock_ledger','product_id','master.product'),
    ('inventory.stock_ledger','variant_id','master.product_variant'),
    ('inventory.stock_reservation','warehouse_id','organization.warehouse'),
    ('inventory.stock_reservation','location_id','warehouse.location'),
    ('inventory.stock_reservation','product_id','master.product'),
    ('inventory.stock_reservation','variant_id','master.product_variant'),
    ('inventory.stock_transfer','company_id','organization.company'),
    ('inventory.stock_transfer','from_warehouse_id','organization.warehouse'),
    ('inventory.stock_transfer','to_warehouse_id','organization.warehouse'),
    ('inventory.stock_adjustment','company_id','organization.company'),
    ('inventory.stock_adjustment','warehouse_id','organization.warehouse'),
    ('purchasing.purchase_order','company_id','organization.company'),
    ('purchasing.goods_receipt','company_id','organization.company'),
    ('purchasing.goods_receipt','warehouse_id','organization.warehouse'),
    ('loyalty.account','customer_id','crm.customer'),
    ('loyalty.transaction','account_id','loyalty.account'),
    ('loyalty.voucher_redemption','customer_id','crm.customer'),
    ('loyalty.voucher_redemption','sales_order_id','sales.sales_order')
  ) AS x(table_name,col_name,ref_table)
  LOOP
    EXECUTE format('DROP TRIGGER IF EXISTS trg_same_tenant_%s ON %s', replace(r.col_name,'_',''), r.table_name);
    EXECUTE format('CREATE TRIGGER trg_same_tenant_%s BEFORE INSERT OR UPDATE OF tenant_id,%I ON %s FOR EACH ROW EXECUTE FUNCTION platform.enforce_same_tenant_fk(%L,%L)', replace(r.col_name,'_',''), r.col_name, r.table_name, r.col_name, r.ref_table);
  END LOOP;
END $$;

-- Every table carrying tenant_id receives tenant RLS. FORCE prevents table-owner bypass.
DO $$
DECLARE r record;
BEGIN
  FOR r IN
    SELECT n.nspname schema_name, c.relname table_name
    FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
    JOIN pg_attribute a ON a.attrelid=c.oid
    WHERE c.relkind='r' AND a.attname='tenant_id'
      AND n.nspname IN ('identity','organization','master','crm','pricing','promotion','sales','pos','payment','inventory','warehouse','purchasing','finance','loyalty','workflow','integration','audit','platform')
    GROUP BY n.nspname,c.relname
  LOOP
    EXECUTE format('ALTER TABLE %I.%I ENABLE ROW LEVEL SECURITY',r.schema_name,r.table_name);
    EXECUTE format('ALTER TABLE %I.%I FORCE ROW LEVEL SECURITY',r.schema_name,r.table_name);
    EXECUTE format('DROP POLICY IF EXISTS tenant_isolation_v11 ON %I.%I',r.schema_name,r.table_name);
    EXECUTE format('CREATE POLICY tenant_isolation_v11 ON %I.%I USING (tenant_id=platform.current_tenant_id()) WITH CHECK (tenant_id=platform.current_tenant_id())',r.schema_name,r.table_name);
  END LOOP;
END $$;

CREATE INDEX IF NOT EXISTS ix_sales_order_tenant_store_date ON sales.sales_order(tenant_id,store_id,order_date DESC);
CREATE INDEX IF NOT EXISTS ix_payment_tenant_status_date ON payment.payment(tenant_id,status,created_at DESC);
CREATE INDEX IF NOT EXISTS ix_stock_tenant_warehouse_product ON inventory.stock(tenant_id,warehouse_id,product_id);
CREATE INDEX IF NOT EXISTS ix_audit_tenant_user_time ON audit.audit_log(tenant_id,user_id,created_at DESC);
CREATE INDEX IF NOT EXISTS ix_outbox_tenant_status_time ON integration.outbox_event(tenant_id,status,occurred_at);
CREATE INDEX IF NOT EXISTS ix_user_scope_tenant_user ON identity.user_scope(tenant_id,user_id);
CREATE UNIQUE INDEX IF NOT EXISTS ux_terminal_device_per_tenant ON organization.terminal(tenant_id,device_identifier) WHERE device_identifier IS NOT NULL;

COMMIT;
