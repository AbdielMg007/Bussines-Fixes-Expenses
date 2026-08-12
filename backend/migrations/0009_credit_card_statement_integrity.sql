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
        'register_credit_card_statement'
    ));

ALTER TABLE credit_card_cycles
    ADD CONSTRAINT credit_card_cycles_owner_account_id_key
    UNIQUE (owner_id, account_id, id);

ALTER TABLE credit_card_statements
    DROP CONSTRAINT credit_card_statements_cycle_id_fkey,
    ADD CONSTRAINT credit_card_statements_cycle_owner_account_fkey
        FOREIGN KEY (owner_id, account_id, cycle_id)
        REFERENCES credit_card_cycles (owner_id, account_id, id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT credit_card_statements_owner_account_cycle_id_key
        UNIQUE (owner_id, account_id, cycle_id, id),
    ADD CONSTRAINT credit_card_statements_superseded_by_fkey
        FOREIGN KEY (owner_id, account_id, cycle_id, superseded_by_id)
        REFERENCES credit_card_statements (owner_id, account_id, cycle_id, id)
        DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE credit_card_payment_intents
    DROP CONSTRAINT credit_card_payment_intents_cycle_id_fkey,
    ADD CONSTRAINT credit_card_payment_intents_cycle_owner_account_fkey
        FOREIGN KEY (owner_id, account_id, cycle_id)
        REFERENCES credit_card_cycles (owner_id, account_id, id)
        ON DELETE RESTRICT;

CREATE FUNCTION runway_validate_credit_card_cycle()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    account_record RECORD;
BEGIN
    SELECT account_type, status
    INTO account_record
    FROM accounts
    WHERE owner_id = NEW.owner_id AND id = NEW.account_id;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'credit card cycle ownership mismatch' USING ERRCODE = '23503';
    END IF;
    IF account_record.account_type <> 'credit_card' THEN
        RAISE EXCEPTION 'credit card cycle requires a credit card account' USING ERRCODE = '23514';
    END IF;
    IF account_record.status <> 'active' THEN
        RAISE EXCEPTION 'credit card cycle requires an active account' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER validate_credit_card_cycle
BEFORE INSERT OR UPDATE OF owner_id, account_id ON credit_card_cycles
FOR EACH ROW EXECUTE FUNCTION runway_validate_credit_card_cycle();

CREATE FUNCTION runway_validate_credit_card_statement_supersession()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    replacement_record RECORD;
BEGIN
    IF NEW.superseded_by_id IS NULL THEN
        RETURN NULL;
    END IF;

    SELECT revision, authority
    INTO replacement_record
    FROM credit_card_statements
    WHERE owner_id = NEW.owner_id
      AND account_id = NEW.account_id
      AND cycle_id = NEW.cycle_id
      AND id = NEW.superseded_by_id;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'statement superseding revision is missing' USING ERRCODE = '23503';
    END IF;
    IF replacement_record.revision <= NEW.revision THEN
        RAISE EXCEPTION 'statement replacement revision must advance' USING ERRCODE = '23514';
    END IF;
    IF NEW.authority = 'issued' AND replacement_record.authority <> 'issued' THEN
        RAISE EXCEPTION 'an issued statement cannot be replaced by an estimate' USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER validate_credit_card_statement_supersession
AFTER INSERT OR UPDATE OF superseded_at, superseded_by_id ON credit_card_statements
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION runway_validate_credit_card_statement_supersession();
