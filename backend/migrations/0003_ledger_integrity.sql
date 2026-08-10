CREATE TABLE financial_mutations (
    owner_id TEXT NOT NULL REFERENCES owners(id) ON DELETE RESTRICT,
    operation TEXT NOT NULL CHECK (operation IN ('post_transaction', 'create_snapshot', 'create_transfer')),
    idempotency_key TEXT NOT NULL CHECK (
        octet_length(idempotency_key) BETWEEN 1 AND 128
        AND idempotency_key ~ '^[A-Za-z0-9._:-]+$'
    ),
    request_fingerprint BYTEA NOT NULL CHECK (octet_length(request_fingerprint) = 32),
    resource_id TEXT NOT NULL CHECK (length(btrim(resource_id)) > 0),
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (owner_id, operation, idempotency_key)
);

CREATE FUNCTION runway_prevent_row_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '% rows are immutable', TG_TABLE_NAME USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER financial_transactions_are_immutable
BEFORE UPDATE OR DELETE ON financial_transactions
FOR EACH ROW EXECUTE FUNCTION runway_prevent_row_mutation();

CREATE TRIGGER balance_snapshots_are_immutable
BEFORE UPDATE OR DELETE ON balance_snapshots
FOR EACH ROW EXECUTE FUNCTION runway_prevent_row_mutation();

CREATE TRIGGER linked_transfers_are_immutable
BEFORE UPDATE OR DELETE ON linked_transfers
FOR EACH ROW EXECUTE FUNCTION runway_prevent_row_mutation();

CREATE TRIGGER financial_mutations_are_immutable
BEFORE UPDATE OR DELETE ON financial_mutations
FOR EACH ROW EXECUTE FUNCTION runway_prevent_row_mutation();

CREATE FUNCTION runway_preserve_account_economic_identity()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.account_type IS DISTINCT FROM OLD.account_type
       OR NEW.currency IS DISTINCT FROM OLD.currency THEN
        RAISE EXCEPTION 'account type and currency are immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER accounts_preserve_economic_identity
BEFORE UPDATE OF account_type, currency ON accounts
FOR EACH ROW EXECUTE FUNCTION runway_preserve_account_economic_identity();

CREATE FUNCTION runway_validate_financial_transaction_insert()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    account_record RECORD;
    transfer_record RECORD;
    expected_effect TEXT;
BEGIN
    SELECT account_type, currency, status
    INTO account_record
    FROM accounts
    WHERE owner_id = NEW.owner_id AND id = NEW.account_id;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'transaction account ownership mismatch' USING ERRCODE = '23503';
    END IF;
    IF account_record.status <> 'active' THEN
        RAISE EXCEPTION 'cannot post to an archived account' USING ERRCODE = '23514';
    END IF;
    IF NEW.currency <> account_record.currency THEN
        RAISE EXCEPTION 'transaction currency mismatch' USING ERRCODE = '23514';
    END IF;
    IF account_record.account_type IN ('cash', 'bank')
       AND NEW.effect NOT IN ('asset_inflow', 'asset_outflow') THEN
        RAISE EXCEPTION 'transaction effect is incompatible with asset account' USING ERRCODE = '23514';
    END IF;
    IF account_record.account_type IN ('credit_card', 'loan')
       AND NEW.effect NOT IN ('liability_charge', 'liability_payment') THEN
        RAISE EXCEPTION 'transaction effect is incompatible with liability account' USING ERRCODE = '23514';
    END IF;

    IF NEW.transaction_kind = 'transfer' THEN
        SELECT source_account_id, destination_account_id, amount_minor, currency, financial_date
        INTO transfer_record
        FROM linked_transfers
        WHERE owner_id = NEW.owner_id AND id = NEW.transfer_id;

        IF NOT FOUND THEN
            RAISE EXCEPTION 'transfer ownership mismatch' USING ERRCODE = '23503';
        END IF;
        IF NEW.amount_minor <> transfer_record.amount_minor
           OR NEW.currency <> transfer_record.currency
           OR NEW.financial_date <> transfer_record.financial_date THEN
            RAISE EXCEPTION 'transfer transaction does not match transfer economics' USING ERRCODE = '23514';
        END IF;

        IF NEW.account_id = transfer_record.source_account_id THEN
            expected_effect := 'asset_outflow';
        ELSIF NEW.account_id = transfer_record.destination_account_id THEN
            IF account_record.account_type IN ('cash', 'bank') THEN
                expected_effect := 'asset_inflow';
            ELSE
                expected_effect := 'liability_payment';
            END IF;
        ELSE
            RAISE EXCEPTION 'transfer transaction account is not a transfer endpoint' USING ERRCODE = '23514';
        END IF;

        IF NEW.effect <> expected_effect THEN
            RAISE EXCEPTION 'transfer transaction effect is incorrect' USING ERRCODE = '23514';
        END IF;
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER validate_financial_transaction_insert
BEFORE INSERT ON financial_transactions
FOR EACH ROW EXECUTE FUNCTION runway_validate_financial_transaction_insert();

CREATE FUNCTION runway_validate_balance_snapshot_insert()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    account_record RECORD;
BEGIN
    SELECT account_type, currency, status
    INTO account_record
    FROM accounts
    WHERE owner_id = NEW.owner_id AND id = NEW.account_id;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'snapshot account ownership mismatch' USING ERRCODE = '23503';
    END IF;
    IF account_record.status <> 'active' THEN
        RAISE EXCEPTION 'cannot snapshot an archived account' USING ERRCODE = '23514';
    END IF;
    IF NEW.currency <> account_record.currency THEN
        RAISE EXCEPTION 'snapshot currency mismatch' USING ERRCODE = '23514';
    END IF;
    IF account_record.account_type IN ('credit_card', 'loan') AND NEW.balance_minor < 0 THEN
        RAISE EXCEPTION 'liability snapshot cannot be negative' USING ERRCODE = '23514';
    END IF;
    IF NEW.cutoff_sequence > 0 AND NOT EXISTS (
        SELECT 1
        FROM financial_transactions
        WHERE owner_id = NEW.owner_id
          AND account_id = NEW.account_id
          AND ledger_sequence = NEW.cutoff_sequence
    ) THEN
        RAISE EXCEPTION 'snapshot cutoff does not belong to the account' USING ERRCODE = '23514';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER validate_balance_snapshot_insert
BEFORE INSERT ON balance_snapshots
FOR EACH ROW EXECUTE FUNCTION runway_validate_balance_snapshot_insert();

CREATE FUNCTION runway_require_complete_linked_transfer()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    total_entries INTEGER;
    source_entries INTEGER;
    destination_entries INTEGER;
BEGIN
    SELECT
        count(*),
        count(*) FILTER (WHERE account_id = NEW.source_account_id),
        count(*) FILTER (WHERE account_id = NEW.destination_account_id)
    INTO total_entries, source_entries, destination_entries
    FROM financial_transactions
    WHERE owner_id = NEW.owner_id AND transfer_id = NEW.id;

    IF total_entries <> 2 OR source_entries <> 1 OR destination_entries <> 1 THEN
        RAISE EXCEPTION 'linked transfer must have exactly its two intended entries' USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER linked_transfer_must_be_complete
AFTER INSERT ON linked_transfers
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION runway_require_complete_linked_transfer();
