CREATE TABLE credit_card_cycles (
 id TEXT PRIMARY KEY, owner_id TEXT NOT NULL, account_id TEXT NOT NULL,
 cycle_start DATE NOT NULL, cycle_end DATE NOT NULL, created_at TIMESTAMPTZ NOT NULL,
 UNIQUE(owner_id, account_id, cycle_start, cycle_end),
 FOREIGN KEY(owner_id, account_id) REFERENCES accounts(owner_id,id) ON DELETE RESTRICT,
 CHECK(cycle_start <= cycle_end)
);
CREATE TABLE credit_card_statements (
 id TEXT PRIMARY KEY, owner_id TEXT NOT NULL, account_id TEXT NOT NULL, cycle_id TEXT NOT NULL,
 revision BIGINT NOT NULL CHECK(revision>0), authority TEXT NOT NULL CHECK(authority IN ('estimated','issued')),
 statement_balance_minor BIGINT NOT NULL CHECK(statement_balance_minor>=0), currency TEXT NOT NULL CHECK(currency='MXN'),
 minimum_payment_minor BIGINT NULL CHECK(minimum_payment_minor>=0 AND minimum_payment_minor<=statement_balance_minor),
 payment_to_avoid_interest_minor BIGINT NULL CHECK(payment_to_avoid_interest_minor>=0 AND payment_to_avoid_interest_minor<=statement_balance_minor),
 due_date DATE NOT NULL, created_at TIMESTAMPTZ NOT NULL, superseded_at TIMESTAMPTZ NULL, superseded_by_id TEXT NULL,
 UNIQUE(cycle_id,revision), FOREIGN KEY(owner_id,account_id) REFERENCES accounts(owner_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(cycle_id) REFERENCES credit_card_cycles(id) ON DELETE RESTRICT,
 CHECK((superseded_at IS NULL) = (superseded_by_id IS NULL))
);
CREATE UNIQUE INDEX one_active_credit_card_statement ON credit_card_statements(cycle_id) WHERE superseded_at IS NULL;
CREATE TABLE credit_card_payment_intents (
 id TEXT PRIMARY KEY, owner_id TEXT NOT NULL, account_id TEXT NOT NULL, cycle_id TEXT NOT NULL UNIQUE,
 amount_minor BIGINT NOT NULL CHECK(amount_minor>0), currency TEXT NOT NULL CHECK(currency='MXN'), planned_date DATE NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('active','needs_review','cancelled')), version BIGINT NOT NULL CHECK(version>0), created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL,
 FOREIGN KEY(owner_id,account_id) REFERENCES accounts(owner_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(cycle_id) REFERENCES credit_card_cycles(id) ON DELETE RESTRICT
);
CREATE FUNCTION runway_immutable_credit_card_statement() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'credit card statements are immutable'; END IF;
 IF OLD.superseded_at IS NULL AND NEW.superseded_at IS NOT NULL AND NEW.superseded_by_id IS NOT NULL
    AND NEW.id=OLD.id AND NEW.owner_id=OLD.owner_id AND NEW.account_id=OLD.account_id AND NEW.cycle_id=OLD.cycle_id AND NEW.revision=OLD.revision AND NEW.authority=OLD.authority AND NEW.statement_balance_minor=OLD.statement_balance_minor AND NEW.currency=OLD.currency AND NEW.minimum_payment_minor IS NOT DISTINCT FROM OLD.minimum_payment_minor AND NEW.payment_to_avoid_interest_minor IS NOT DISTINCT FROM OLD.payment_to_avoid_interest_minor AND NEW.due_date=OLD.due_date AND NEW.created_at=OLD.created_at THEN RETURN NEW; END IF;
 RAISE EXCEPTION 'credit card statements are immutable';
END; $$;
CREATE TRIGGER immutable_credit_card_statement BEFORE UPDATE OR DELETE ON credit_card_statements FOR EACH ROW EXECUTE FUNCTION runway_immutable_credit_card_statement();
