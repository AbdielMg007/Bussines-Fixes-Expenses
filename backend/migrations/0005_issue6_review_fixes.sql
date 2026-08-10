ALTER TABLE obligations
    ADD COLUMN inactive_from DATE
        CHECK (inactive_from BETWEEN DATE '0001-01-01' AND DATE '9999-12-31');

ALTER TABLE obligations
    ADD CONSTRAINT obligations_inactive_state_check CHECK (
        (status = 'active' AND inactive_from IS NULL)
        OR (status = 'archived' AND inactive_from IS NOT NULL)
    );

ALTER TABLE scheduled_cash_flows
    DROP CONSTRAINT scheduled_cash_flows_source_kind_check;

ALTER TABLE scheduled_cash_flows
    ADD CONSTRAINT scheduled_cash_flows_source_kind_check CHECK (
        source_kind IN ('manual_expected_income', 'manual_other')
    );
