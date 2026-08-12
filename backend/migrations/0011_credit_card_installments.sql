ALTER TABLE financial_mutations
    DROP CONSTRAINT financial_mutations_operation_check;

ALTER TABLE financial_mutations
    ADD CONSTRAINT financial_mutations_operation_check CHECK (operation IN (
        'post_transaction',
        'create_snapshot',
        'create_transfer',
        'create_obligation',
        'archive_obligation',
        'create_scheduled_flow',
        'cancel_scheduled_flow',
        'create_receivable',
        'collect_receivable',
        'cancel_receivable',
        'register_credit_card_statement',
        'create_installment_plan',
        'record_installment_principal_payment'
    ));

ALTER TABLE financial_transactions
    ADD CONSTRAINT financial_transactions_owner_account_id_key
    UNIQUE (owner_id, account_id, id);

CREATE TABLE credit_card_installment_plans (
    id TEXT PRIMARY KEY CHECK (length(btrim(id)) > 0),
    owner_id TEXT NOT NULL,
    account_id TEXT NOT NULL,
    description TEXT NOT NULL CHECK (length(btrim(description)) BETWEEN 1 AND 500),
    purchase_transaction_id TEXT NOT NULL,
    original_principal_minor BIGINT NOT NULL CHECK (original_principal_minor > 0),
    currency TEXT NOT NULL CHECK (currency = 'MXN'),
    installment_count INTEGER NOT NULL CHECK (installment_count BETWEEN 1 AND 600),
    first_cycle_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active', 'completed')),
    schedule_version INTEGER NOT NULL CHECK (schedule_version = 1),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (owner_id, id),
    UNIQUE (owner_id, account_id, id),
    UNIQUE (owner_id, account_id, purchase_transaction_id),
    FOREIGN KEY (owner_id, account_id) REFERENCES accounts(owner_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (owner_id, account_id, purchase_transaction_id)
        REFERENCES financial_transactions(owner_id, account_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (owner_id, account_id, first_cycle_id)
        REFERENCES credit_card_cycles(owner_id, account_id, id) ON DELETE RESTRICT,
    CHECK (updated_at >= created_at)
);

CREATE TABLE credit_card_installment_allocations (
    id TEXT PRIMARY KEY CHECK (length(btrim(id)) > 0),
    owner_id TEXT NOT NULL,
    account_id TEXT NOT NULL,
    plan_id TEXT NOT NULL,
    cycle_id TEXT NOT NULL,
    installment_number INTEGER NOT NULL CHECK (installment_number > 0),
    schedule_version INTEGER NOT NULL CHECK (schedule_version = 1),
    principal_minor BIGINT NOT NULL CHECK (principal_minor > 0),
    currency TEXT NOT NULL CHECK (currency = 'MXN'),
    status TEXT NOT NULL CHECK (status IN ('pending', 'statement_allocated', 'paid', 'superseded')),
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (owner_id, id),
    UNIQUE (owner_id, account_id, plan_id, id),
    UNIQUE (plan_id, schedule_version, installment_number),
    FOREIGN KEY (owner_id, account_id, plan_id)
        REFERENCES credit_card_installment_plans(owner_id, account_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (owner_id, account_id, cycle_id)
        REFERENCES credit_card_cycles(owner_id, account_id, id) ON DELETE RESTRICT
);

CREATE TABLE credit_card_installment_principal_payments (
    id TEXT PRIMARY KEY CHECK (length(btrim(id)) > 0),
    owner_id TEXT NOT NULL,
    account_id TEXT NOT NULL,
    plan_id TEXT NOT NULL,
    allocation_id TEXT NOT NULL,
    amount_minor BIGINT NOT NULL CHECK (amount_minor > 0),
    currency TEXT NOT NULL CHECK (currency = 'MXN'),
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (owner_id, id),
    FOREIGN KEY (owner_id, account_id, plan_id, allocation_id)
        REFERENCES credit_card_installment_allocations(owner_id, account_id, plan_id, id) ON DELETE RESTRICT
);

CREATE INDEX credit_card_installment_plans_owner_account_idx
    ON credit_card_installment_plans(owner_id, account_id, created_at, id);
CREATE INDEX credit_card_installment_allocations_owner_plan_idx
    ON credit_card_installment_allocations(owner_id, plan_id, schedule_version, installment_number, id);
CREATE INDEX credit_card_installment_principal_payments_allocation_idx
    ON credit_card_installment_principal_payments(owner_id, allocation_id);

CREATE FUNCTION runway_validate_credit_card_installment_plan()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    account_record RECORD;
BEGIN
    SELECT account_type, status, currency
    INTO account_record
    FROM accounts
    WHERE owner_id = NEW.owner_id AND id = NEW.account_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'installment plan ownership mismatch' USING ERRCODE = '23503';
    END IF;
    IF account_record.account_type <> 'credit_card' OR account_record.status <> 'active' THEN
        RAISE EXCEPTION 'installment plan requires an active credit card account' USING ERRCODE = '23514';
    END IF;
    IF account_record.currency <> NEW.currency THEN
        RAISE EXCEPTION 'installment plan currency mismatch' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER validate_credit_card_installment_plan
BEFORE INSERT ON credit_card_installment_plans
FOR EACH ROW EXECUTE FUNCTION runway_validate_credit_card_installment_plan();

CREATE FUNCTION runway_validate_credit_card_installment_plan_purchase()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    transaction_record RECORD;
BEGIN
    SELECT amount_minor, currency, effect
    INTO transaction_record
    FROM financial_transactions
    WHERE owner_id = NEW.owner_id
      AND account_id = NEW.account_id
      AND id = NEW.purchase_transaction_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'installment plan purchase transaction ownership mismatch' USING ERRCODE = '23503';
    END IF;
    IF transaction_record.effect <> 'liability_charge'
       OR transaction_record.currency <> NEW.currency
       OR transaction_record.amount_minor <> NEW.original_principal_minor THEN
        RAISE EXCEPTION 'installment plan must reference its matching card purchase principal' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER validate_credit_card_installment_plan_purchase
BEFORE INSERT ON credit_card_installment_plans
FOR EACH ROW EXECUTE FUNCTION runway_validate_credit_card_installment_plan_purchase();

CREATE FUNCTION runway_validate_credit_card_installment_allocation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    plan_record RECORD;
BEGIN
    SELECT original_principal_minor, currency, installment_count, schedule_version, status
    INTO plan_record
    FROM credit_card_installment_plans
    WHERE owner_id = NEW.owner_id AND account_id = NEW.account_id AND id = NEW.plan_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'installment allocation plan ownership mismatch' USING ERRCODE = '23503';
    END IF;
    IF NEW.currency <> plan_record.currency
       OR NEW.installment_number > plan_record.installment_count
       OR NEW.schedule_version <> plan_record.schedule_version
       OR NEW.status NOT IN ('pending', 'statement_allocated') THEN
        RAISE EXCEPTION 'invalid installment allocation' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER validate_credit_card_installment_allocation
BEFORE INSERT ON credit_card_installment_allocations
FOR EACH ROW EXECUTE FUNCTION runway_validate_credit_card_installment_allocation();

CREATE FUNCTION runway_validate_credit_card_installment_schedule()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    plan_record RECORD;
    allocation_total BIGINT;
    allocation_count INTEGER;
    target_plan_id TEXT;
BEGIN
    IF TG_TABLE_NAME = 'credit_card_installment_plans' THEN
        target_plan_id := COALESCE(NEW.id, OLD.id);
    ELSE
        target_plan_id := COALESCE(NEW.plan_id, OLD.plan_id);
    END IF;
    SELECT original_principal_minor, installment_count
    INTO plan_record
    FROM credit_card_installment_plans
    WHERE id = target_plan_id;
    IF NOT FOUND THEN
        RETURN NULL;
    END IF;
    SELECT COALESCE(SUM(principal_minor), 0), count(*)
    INTO allocation_total, allocation_count
    FROM credit_card_installment_allocations
    WHERE plan_id = target_plan_id AND schedule_version = 1;
    IF allocation_total <> plan_record.original_principal_minor OR allocation_count <> plan_record.installment_count THEN
        RAISE EXCEPTION 'installment schedule does not conserve principal' USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER validate_credit_card_installment_plan_schedule
AFTER INSERT ON credit_card_installment_plans
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION runway_validate_credit_card_installment_schedule();

CREATE CONSTRAINT TRIGGER validate_credit_card_installment_allocation_schedule
AFTER INSERT ON credit_card_installment_allocations
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION runway_validate_credit_card_installment_schedule();

CREATE FUNCTION runway_preserve_credit_card_installment_plan()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    paid_total BIGINT;
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'credit card installment plans are immutable' USING ERRCODE = '55000';
    END IF;
    IF NEW.id = OLD.id
       AND NEW.owner_id = OLD.owner_id
       AND NEW.account_id = OLD.account_id
       AND NEW.description = OLD.description
       AND NEW.purchase_transaction_id = OLD.purchase_transaction_id
       AND NEW.original_principal_minor = OLD.original_principal_minor
       AND NEW.currency = OLD.currency
       AND NEW.installment_count = OLD.installment_count
       AND NEW.first_cycle_id = OLD.first_cycle_id
       AND NEW.schedule_version = OLD.schedule_version
       AND OLD.status = 'active'
       AND NEW.status = 'completed'
       AND NEW.updated_at >= OLD.updated_at THEN
        SELECT COALESCE(SUM(amount_minor), 0) INTO paid_total
        FROM credit_card_installment_principal_payments WHERE plan_id = OLD.id;
        IF paid_total = OLD.original_principal_minor THEN
            RETURN NEW;
        END IF;
    END IF;
    RAISE EXCEPTION 'credit card installment plans are immutable' USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER preserve_credit_card_installment_plan
BEFORE UPDATE OR DELETE ON credit_card_installment_plans
FOR EACH ROW EXECUTE FUNCTION runway_preserve_credit_card_installment_plan();

CREATE FUNCTION runway_preserve_credit_card_installment_allocation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    paid_total BIGINT;
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'credit card installment allocations are immutable' USING ERRCODE = '55000';
    END IF;
    IF NEW.id = OLD.id
       AND NEW.owner_id = OLD.owner_id
       AND NEW.account_id = OLD.account_id
       AND NEW.plan_id = OLD.plan_id
       AND NEW.cycle_id = OLD.cycle_id
       AND NEW.installment_number = OLD.installment_number
       AND NEW.schedule_version = OLD.schedule_version
       AND NEW.principal_minor = OLD.principal_minor
       AND NEW.currency = OLD.currency
       AND NEW.created_at = OLD.created_at THEN
        IF OLD.status = 'pending' AND NEW.status = 'statement_allocated' THEN
            RETURN NEW;
        END IF;
        IF OLD.status IN ('pending', 'statement_allocated') AND NEW.status = 'paid' THEN
            SELECT COALESCE(SUM(amount_minor), 0) INTO paid_total
            FROM credit_card_installment_principal_payments WHERE allocation_id = OLD.id;
            IF paid_total = OLD.principal_minor THEN
                RETURN NEW;
            END IF;
        END IF;
    END IF;
    RAISE EXCEPTION 'credit card installment allocations are immutable' USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER preserve_credit_card_installment_allocation
BEFORE UPDATE OR DELETE ON credit_card_installment_allocations
FOR EACH ROW EXECUTE FUNCTION runway_preserve_credit_card_installment_allocation();

CREATE FUNCTION runway_validate_credit_card_installment_principal_payment()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    allocation_record RECORD;
    paid_total BIGINT;
BEGIN
    SELECT principal_minor, currency, status
    INTO allocation_record
    FROM credit_card_installment_allocations
    WHERE owner_id = NEW.owner_id
      AND account_id = NEW.account_id
      AND plan_id = NEW.plan_id
      AND id = NEW.allocation_id
    FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'installment principal payment ownership mismatch' USING ERRCODE = '23503';
    END IF;
    IF allocation_record.status IN ('paid', 'superseded') OR allocation_record.currency <> NEW.currency THEN
        RAISE EXCEPTION 'invalid installment principal payment' USING ERRCODE = '23514';
    END IF;
    SELECT COALESCE(SUM(amount_minor), 0)
    INTO paid_total
    FROM credit_card_installment_principal_payments
    WHERE allocation_id = NEW.allocation_id;
    IF paid_total + NEW.amount_minor > allocation_record.principal_minor THEN
        RAISE EXCEPTION 'installment principal payment exceeds allocation' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER validate_credit_card_installment_principal_payment
BEFORE INSERT ON credit_card_installment_principal_payments
FOR EACH ROW EXECUTE FUNCTION runway_validate_credit_card_installment_principal_payment();

CREATE TRIGGER credit_card_installment_principal_payments_are_immutable
BEFORE UPDATE OR DELETE ON credit_card_installment_principal_payments
FOR EACH ROW EXECUTE FUNCTION runway_prevent_row_mutation();
