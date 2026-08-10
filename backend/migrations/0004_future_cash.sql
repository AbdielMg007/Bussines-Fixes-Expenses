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
        'cancel_receivable'
    ));

CREATE TABLE obligations (
    id TEXT PRIMARY KEY CHECK (length(btrim(id)) > 0),
    owner_id TEXT NOT NULL REFERENCES owners(id) ON DELETE RESTRICT,
    display_name TEXT NOT NULL CHECK (length(btrim(display_name)) BETWEEN 1 AND 120),
    amount_minor BIGINT NOT NULL CHECK (amount_minor > 0),
    currency TEXT NOT NULL CHECK (currency = 'MXN'),
    direction TEXT NOT NULL DEFAULT 'outflow' CHECK (direction = 'outflow'),
    recurrence TEXT NOT NULL CHECK (recurrence IN ('one_time', 'weekly', 'biweekly', 'monthly')),
    start_date DATE NOT NULL CHECK (start_date BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'),
    end_date DATE CHECK (end_date BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'),
    status TEXT NOT NULL CHECK (status IN ('active', 'archived')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (owner_id, id),
    CHECK (end_date IS NULL OR end_date >= start_date),
    CHECK (updated_at >= created_at)
);

CREATE INDEX obligations_owner_created_idx
    ON obligations (owner_id, created_at, id);

CREATE TABLE scheduled_cash_flows (
    id TEXT PRIMARY KEY CHECK (length(btrim(id)) > 0),
    owner_id TEXT NOT NULL REFERENCES owners(id) ON DELETE RESTRICT,
    source_kind TEXT NOT NULL CHECK (source_kind IN ('obligation', 'receivable', 'manual_expected_income', 'manual_other')),
    source_id TEXT NOT NULL CHECK (length(btrim(source_id)) > 0),
    amount_minor BIGINT NOT NULL CHECK (amount_minor > 0),
    currency TEXT NOT NULL CHECK (currency = 'MXN'),
    direction TEXT NOT NULL CHECK (direction IN ('inflow', 'outflow')),
    financial_date DATE NOT NULL CHECK (financial_date BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'),
    status TEXT NOT NULL CHECK (status IN ('scheduled', 'settled', 'cancelled')),
    amount_provenance TEXT NOT NULL CHECK (amount_provenance IN ('exact', 'estimated')),
    date_provenance TEXT NOT NULL CHECK (date_provenance IN ('exact', 'estimated', 'assumed_by_scenario')),
    inclusion_eligibility TEXT NOT NULL CHECK (inclusion_eligibility IN ('eligible', 'excluded')),
    settlement_transaction_id TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (owner_id, id),
    FOREIGN KEY (owner_id, settlement_transaction_id)
        REFERENCES financial_transactions(owner_id, id) ON DELETE RESTRICT,
    CHECK (
        (status = 'settled' AND settlement_transaction_id IS NOT NULL)
        OR (status IN ('scheduled', 'cancelled') AND settlement_transaction_id IS NULL)
    ),
    CHECK (
        source_kind NOT IN ('manual_expected_income', 'manual_other')
        OR source_id = id
    ),
    CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX scheduled_cash_flows_settlement_idx
    ON scheduled_cash_flows (owner_id, settlement_transaction_id)
    WHERE settlement_transaction_id IS NOT NULL;
CREATE INDEX scheduled_cash_flows_owner_date_idx
    ON scheduled_cash_flows (owner_id, financial_date, created_at, id);

CREATE TABLE receivables (
    id TEXT PRIMARY KEY CHECK (length(btrim(id)) > 0),
    owner_id TEXT NOT NULL REFERENCES owners(id) ON DELETE RESTRICT,
    display_name TEXT NOT NULL CHECK (length(btrim(display_name)) BETWEEN 1 AND 120),
    original_amount_minor BIGINT NOT NULL CHECK (original_amount_minor > 0),
    collected_amount_minor BIGINT NOT NULL DEFAULT 0 CHECK (collected_amount_minor >= 0),
    currency TEXT NOT NULL CHECK (currency = 'MXN'),
    status TEXT NOT NULL CHECK (status IN ('open', 'partially_collected', 'collected', 'cancelled')),
    expected_date DATE CHECK (expected_date BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'),
    certainty TEXT NOT NULL CHECK (certainty IN ('confirmed', 'expected', 'uncertain')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (owner_id, id),
    CHECK (collected_amount_minor <= original_amount_minor),
    CHECK (
        (status = 'open' AND collected_amount_minor = 0)
        OR (status = 'partially_collected' AND collected_amount_minor > 0 AND collected_amount_minor < original_amount_minor)
        OR (status = 'collected' AND collected_amount_minor = original_amount_minor)
        OR (status = 'cancelled' AND collected_amount_minor < original_amount_minor)
    ),
    CHECK (updated_at >= created_at)
);

CREATE INDEX receivables_owner_created_idx
    ON receivables (owner_id, created_at, id);

CREATE TABLE receivable_collections (
    id TEXT PRIMARY KEY CHECK (length(btrim(id)) > 0),
    owner_id TEXT NOT NULL REFERENCES owners(id) ON DELETE RESTRICT,
    receivable_id TEXT NOT NULL,
    amount_minor BIGINT NOT NULL CHECK (amount_minor > 0),
    currency TEXT NOT NULL CHECK (currency = 'MXN'),
    collected_before_minor BIGINT NOT NULL CHECK (collected_before_minor >= 0),
    collected_after_minor BIGINT NOT NULL CHECK (collected_after_minor > 0),
    outstanding_after_minor BIGINT NOT NULL CHECK (outstanding_after_minor >= 0),
    resulting_status TEXT NOT NULL CHECK (resulting_status IN ('partially_collected', 'collected')),
    ledger_transaction_id TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (owner_id, id),
    FOREIGN KEY (owner_id, receivable_id)
        REFERENCES receivables(owner_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (owner_id, ledger_transaction_id)
        REFERENCES financial_transactions(owner_id, id) ON DELETE RESTRICT,
    CHECK (collected_after_minor = collected_before_minor + amount_minor),
    CHECK (
        (resulting_status = 'collected' AND outstanding_after_minor = 0)
        OR (resulting_status = 'partially_collected' AND outstanding_after_minor > 0)
    )
);

CREATE UNIQUE INDEX receivable_collections_ledger_link_idx
    ON receivable_collections (owner_id, ledger_transaction_id)
    WHERE ledger_transaction_id IS NOT NULL;
CREATE INDEX receivable_collections_receivable_idx
    ON receivable_collections (owner_id, receivable_id, created_at, id);

CREATE FUNCTION runway_validate_obligation_update()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.owner_id IS DISTINCT FROM OLD.owner_id
       OR NEW.display_name IS DISTINCT FROM OLD.display_name
       OR NEW.amount_minor IS DISTINCT FROM OLD.amount_minor
       OR NEW.currency IS DISTINCT FROM OLD.currency
       OR NEW.direction IS DISTINCT FROM OLD.direction
       OR NEW.recurrence IS DISTINCT FROM OLD.recurrence
       OR NEW.start_date IS DISTINCT FROM OLD.start_date
       OR NEW.end_date IS DISTINCT FROM OLD.end_date
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'obligation economic fields are immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status <> 'active' OR NEW.status <> 'archived' OR NEW.updated_at < OLD.updated_at THEN
        RAISE EXCEPTION 'invalid obligation status transition' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION runway_validate_obligation_insert()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.status <> 'active' THEN
        RAISE EXCEPTION 'new obligation must be active' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER validate_obligation_insert
BEFORE INSERT ON obligations
FOR EACH ROW EXECUTE FUNCTION runway_validate_obligation_insert();
CREATE TRIGGER obligations_are_not_deleted
BEFORE DELETE ON obligations
FOR EACH ROW EXECUTE FUNCTION runway_prevent_row_mutation();
CREATE TRIGGER validate_obligation_update
BEFORE UPDATE ON obligations
FOR EACH ROW EXECUTE FUNCTION runway_validate_obligation_update();

CREATE FUNCTION runway_validate_scheduled_flow_source()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.status <> 'scheduled' OR NEW.settlement_transaction_id IS NOT NULL THEN
        RAISE EXCEPTION 'new scheduled flow must be unsettled and scheduled' USING ERRCODE = '23514';
    END IF;
    IF NEW.source_kind = 'obligation' AND NOT EXISTS (
        SELECT 1 FROM obligations WHERE owner_id = NEW.owner_id AND id = NEW.source_id
    ) THEN
        RAISE EXCEPTION 'scheduled flow obligation ownership mismatch' USING ERRCODE = '23503';
    END IF;
    IF NEW.source_kind = 'receivable' AND NOT EXISTS (
        SELECT 1 FROM receivables WHERE owner_id = NEW.owner_id AND id = NEW.source_id
    ) THEN
        RAISE EXCEPTION 'scheduled flow receivable ownership mismatch' USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION runway_validate_scheduled_flow_update()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    transaction_record RECORD;
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.owner_id IS DISTINCT FROM OLD.owner_id
       OR NEW.source_kind IS DISTINCT FROM OLD.source_kind
       OR NEW.source_id IS DISTINCT FROM OLD.source_id
       OR NEW.amount_minor IS DISTINCT FROM OLD.amount_minor
       OR NEW.currency IS DISTINCT FROM OLD.currency
       OR NEW.direction IS DISTINCT FROM OLD.direction
       OR NEW.financial_date IS DISTINCT FROM OLD.financial_date
       OR NEW.amount_provenance IS DISTINCT FROM OLD.amount_provenance
       OR NEW.date_provenance IS DISTINCT FROM OLD.date_provenance
       OR NEW.inclusion_eligibility IS DISTINCT FROM OLD.inclusion_eligibility
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'scheduled flow economic fields are immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status <> 'scheduled' OR NEW.status NOT IN ('settled', 'cancelled') OR NEW.updated_at < OLD.updated_at THEN
        RAISE EXCEPTION 'invalid scheduled flow status transition' USING ERRCODE = '23514';
    END IF;
    IF NEW.status = 'settled' THEN
        SELECT amount_minor, currency, effect
        INTO transaction_record
        FROM financial_transactions
        WHERE owner_id = NEW.owner_id AND id = NEW.settlement_transaction_id;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'scheduled flow settlement ownership mismatch' USING ERRCODE = '23503';
        END IF;
        IF transaction_record.amount_minor <> NEW.amount_minor
           OR transaction_record.currency <> NEW.currency
           OR (NEW.direction = 'inflow' AND transaction_record.effect <> 'asset_inflow')
           OR (NEW.direction = 'outflow' AND transaction_record.effect <> 'asset_outflow') THEN
            RAISE EXCEPTION 'scheduled flow settlement is incompatible' USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER validate_scheduled_flow_source
BEFORE INSERT ON scheduled_cash_flows
FOR EACH ROW EXECUTE FUNCTION runway_validate_scheduled_flow_source();
CREATE TRIGGER scheduled_cash_flows_are_not_deleted
BEFORE DELETE ON scheduled_cash_flows
FOR EACH ROW EXECUTE FUNCTION runway_prevent_row_mutation();
CREATE TRIGGER validate_scheduled_flow_update
BEFORE UPDATE ON scheduled_cash_flows
FOR EACH ROW EXECUTE FUNCTION runway_validate_scheduled_flow_update();

CREATE FUNCTION runway_validate_receivable_update()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.owner_id IS DISTINCT FROM OLD.owner_id
       OR NEW.display_name IS DISTINCT FROM OLD.display_name
       OR NEW.original_amount_minor IS DISTINCT FROM OLD.original_amount_minor
       OR NEW.currency IS DISTINCT FROM OLD.currency
       OR NEW.expected_date IS DISTINCT FROM OLD.expected_date
       OR NEW.certainty IS DISTINCT FROM OLD.certainty
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'receivable economic fields are immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status IN ('collected', 'cancelled') OR NEW.updated_at < OLD.updated_at THEN
        RAISE EXCEPTION 'invalid receivable status transition' USING ERRCODE = '23514';
    END IF;
    IF NEW.status = 'cancelled' AND NEW.collected_amount_minor <> OLD.collected_amount_minor THEN
        RAISE EXCEPTION 'cancelling cannot collect a receivable' USING ERRCODE = '23514';
    END IF;
    IF NEW.status <> 'cancelled' AND NEW.collected_amount_minor <= OLD.collected_amount_minor THEN
        RAISE EXCEPTION 'collection must increase collected amount' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION runway_validate_receivable_insert()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.status <> 'open' OR NEW.collected_amount_minor <> 0 THEN
        RAISE EXCEPTION 'new receivable must begin open and uncollected' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION runway_validate_collection_insert()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    receivable_record RECORD;
    transaction_record RECORD;
BEGIN
    SELECT original_amount_minor, collected_amount_minor, currency, status
    INTO receivable_record
    FROM receivables
    WHERE owner_id = NEW.owner_id AND id = NEW.receivable_id;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'collection receivable ownership mismatch' USING ERRCODE = '23503';
    END IF;
    IF NEW.currency <> receivable_record.currency
       OR NEW.collected_after_minor <> receivable_record.collected_amount_minor
       OR NEW.outstanding_after_minor <> receivable_record.original_amount_minor - receivable_record.collected_amount_minor
       OR NEW.resulting_status <> receivable_record.status THEN
        RAISE EXCEPTION 'collection does not reconcile to receivable' USING ERRCODE = '23514';
    END IF;

    IF NEW.ledger_transaction_id IS NOT NULL THEN
        SELECT amount_minor, currency, effect
        INTO transaction_record
        FROM financial_transactions
        WHERE owner_id = NEW.owner_id AND id = NEW.ledger_transaction_id;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'collection ledger ownership mismatch' USING ERRCODE = '23503';
        END IF;
        IF transaction_record.amount_minor <> NEW.amount_minor
           OR transaction_record.currency <> NEW.currency
           OR transaction_record.effect <> 'asset_inflow' THEN
            RAISE EXCEPTION 'collection ledger transaction is incompatible' USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION runway_require_receivable_conservation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    collection_total BIGINT;
BEGIN
    SELECT COALESCE(sum(amount_minor), 0)
    INTO collection_total
    FROM receivable_collections
    WHERE owner_id = NEW.owner_id AND receivable_id = NEW.id;

    IF collection_total <> NEW.collected_amount_minor THEN
        RAISE EXCEPTION 'receivable collections do not conserve collected amount' USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER validate_receivable_insert
BEFORE INSERT ON receivables
FOR EACH ROW EXECUTE FUNCTION runway_validate_receivable_insert();
CREATE TRIGGER receivables_are_not_deleted
BEFORE DELETE ON receivables
FOR EACH ROW EXECUTE FUNCTION runway_prevent_row_mutation();
CREATE TRIGGER validate_receivable_update
BEFORE UPDATE ON receivables
FOR EACH ROW EXECUTE FUNCTION runway_validate_receivable_update();
CREATE CONSTRAINT TRIGGER receivable_collection_conservation
AFTER UPDATE OF collected_amount_minor, status ON receivables
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION runway_require_receivable_conservation();

CREATE TRIGGER validate_collection_insert
BEFORE INSERT ON receivable_collections
FOR EACH ROW EXECUTE FUNCTION runway_validate_collection_insert();
CREATE TRIGGER receivable_collections_are_immutable
BEFORE UPDATE OR DELETE ON receivable_collections
FOR EACH ROW EXECUTE FUNCTION runway_prevent_row_mutation();
