ALTER TABLE credit_card_installment_allocations
    ADD CONSTRAINT credit_card_installment_allocations_plan_version_cycle_key
    UNIQUE (plan_id, schedule_version, cycle_id);

CREATE FUNCTION runway_validate_credit_card_installment_allocation_lifecycle()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_allocation_id TEXT;
    allocation_record RECORD;
    paid_total BIGINT;
BEGIN
    IF TG_TABLE_NAME = 'credit_card_installment_principal_payments' THEN
        target_allocation_id := COALESCE(NEW.allocation_id, OLD.allocation_id);
    ELSE
        target_allocation_id := COALESCE(NEW.id, OLD.id);
    END IF;

    SELECT principal_minor, status
    INTO allocation_record
    FROM credit_card_installment_allocations
    WHERE id = target_allocation_id;
    IF NOT FOUND THEN
        RETURN NULL;
    END IF;

    SELECT COALESCE(SUM(amount_minor), 0)
    INTO paid_total
    FROM credit_card_installment_principal_payments
    WHERE allocation_id = target_allocation_id;

    IF paid_total > allocation_record.principal_minor
       OR (paid_total = allocation_record.principal_minor AND allocation_record.status <> 'paid')
       OR (paid_total < allocation_record.principal_minor AND allocation_record.status = 'paid') THEN
        RAISE EXCEPTION 'installment allocation payment lifecycle is inconsistent' USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;

CREATE FUNCTION runway_validate_credit_card_installment_plan_lifecycle()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_plan_id TEXT;
    plan_record RECORD;
    paid_total BIGINT;
BEGIN
    IF TG_TABLE_NAME = 'credit_card_installment_principal_payments' THEN
        target_plan_id := COALESCE(NEW.plan_id, OLD.plan_id);
    ELSE
        target_plan_id := COALESCE(NEW.id, OLD.id);
    END IF;

    SELECT original_principal_minor, status
    INTO plan_record
    FROM credit_card_installment_plans
    WHERE id = target_plan_id;
    IF NOT FOUND THEN
        RETURN NULL;
    END IF;

    SELECT COALESCE(SUM(amount_minor), 0)
    INTO paid_total
    FROM credit_card_installment_principal_payments
    WHERE plan_id = target_plan_id;

    IF paid_total > plan_record.original_principal_minor
       OR (paid_total = plan_record.original_principal_minor AND plan_record.status <> 'completed')
       OR (paid_total < plan_record.original_principal_minor AND plan_record.status = 'completed') THEN
        RAISE EXCEPTION 'installment plan payment lifecycle is inconsistent' USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER validate_credit_card_installment_allocation_lifecycle_from_payment
AFTER INSERT OR UPDATE OR DELETE ON credit_card_installment_principal_payments
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION runway_validate_credit_card_installment_allocation_lifecycle();

CREATE CONSTRAINT TRIGGER validate_credit_card_installment_allocation_lifecycle_from_allocation
AFTER UPDATE ON credit_card_installment_allocations
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION runway_validate_credit_card_installment_allocation_lifecycle();

CREATE CONSTRAINT TRIGGER validate_credit_card_installment_plan_lifecycle_from_payment
AFTER INSERT OR UPDATE OR DELETE ON credit_card_installment_principal_payments
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION runway_validate_credit_card_installment_plan_lifecycle();

CREATE CONSTRAINT TRIGGER validate_credit_card_installment_plan_lifecycle_from_plan
AFTER UPDATE ON credit_card_installment_plans
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION runway_validate_credit_card_installment_plan_lifecycle();
