DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM receivables) THEN
        RAISE EXCEPTION
            'receivable provenance was not captured before migration 0007; reset this pre-release database or reconcile rows explicitly before migrating'
            USING ERRCODE = '23514';
    END IF;
END;
$$;

ALTER TABLE receivables
    ADD COLUMN amount_provenance TEXT NOT NULL
        CHECK (amount_provenance IN ('exact', 'estimated')),
    ADD COLUMN date_provenance TEXT
        CHECK (date_provenance IN ('exact', 'estimated')),
    ADD CONSTRAINT receivables_expected_date_provenance_check CHECK (
        (expected_date IS NULL AND date_provenance IS NULL)
        OR (expected_date IS NOT NULL AND date_provenance IS NOT NULL)
    );

CREATE OR REPLACE FUNCTION runway_validate_receivable_update()
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
       OR NEW.amount_provenance IS DISTINCT FROM OLD.amount_provenance
       OR NEW.date_provenance IS DISTINCT FROM OLD.date_provenance
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
