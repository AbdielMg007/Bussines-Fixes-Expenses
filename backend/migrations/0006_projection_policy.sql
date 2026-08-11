CREATE TABLE projection_policies (
    id TEXT PRIMARY KEY CHECK (length(btrim(id)) > 0),
    owner_id TEXT NOT NULL UNIQUE REFERENCES owners(id) ON DELETE RESTRICT,
    currency TEXT NOT NULL CHECK (currency = 'MXN'),
    horizon_days INTEGER NOT NULL CHECK (horizon_days BETWEEN 1 AND 366),
    reserve_minor BIGINT NOT NULL CHECK (reserve_minor >= 0),
    financial_timezone TEXT NOT NULL CHECK (length(btrim(financial_timezone)) > 0),
    account_selection_mode TEXT NOT NULL CHECK (account_selection_mode IN ('all_active_liquid', 'explicit')),
    inflow_policy TEXT NOT NULL CHECK (inflow_policy IN ('confirmed_only', 'include_expected')),
    same_day_order TEXT NOT NULL CHECK (same_day_order = 'outflows_before_inflows'),
    version BIGINT NOT NULL CHECK (version > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (owner_id, id),
    CHECK (updated_at >= created_at)
);

CREATE TABLE projection_policy_accounts (
    owner_id TEXT NOT NULL,
    policy_id TEXT NOT NULL,
    account_id TEXT NOT NULL,
    PRIMARY KEY (policy_id, account_id),
    FOREIGN KEY (owner_id, policy_id)
        REFERENCES projection_policies(owner_id, id) ON DELETE CASCADE,
    FOREIGN KEY (owner_id, account_id)
        REFERENCES accounts(owner_id, id) ON DELETE RESTRICT
);

CREATE INDEX projection_policy_accounts_owner_idx
    ON projection_policy_accounts (owner_id, policy_id, account_id);

CREATE FUNCTION runway_validate_projection_policy_account()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    policy_record RECORD;
    account_record RECORD;
BEGIN
    SELECT currency, account_selection_mode
    INTO policy_record
    FROM projection_policies
    WHERE owner_id = NEW.owner_id AND id = NEW.policy_id;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'projection policy ownership mismatch' USING ERRCODE = '23503';
    END IF;
    IF policy_record.account_selection_mode <> 'explicit' THEN
        RAISE EXCEPTION 'all-active policy cannot have explicit accounts' USING ERRCODE = '23514';
    END IF;

    SELECT account_type, currency, status
    INTO account_record
    FROM accounts
    WHERE owner_id = NEW.owner_id AND id = NEW.account_id;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'projection account ownership mismatch' USING ERRCODE = '23503';
    END IF;
    IF account_record.status <> 'active'
       OR account_record.account_type NOT IN ('cash', 'bank')
       OR account_record.currency <> policy_record.currency THEN
        RAISE EXCEPTION 'account is not eligible for projection liquidity' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER validate_projection_policy_account
BEFORE INSERT OR UPDATE ON projection_policy_accounts
FOR EACH ROW EXECUTE FUNCTION runway_validate_projection_policy_account();

CREATE FUNCTION runway_validate_projection_policy_mode()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.account_selection_mode = 'all_active_liquid'
       AND EXISTS (
           SELECT 1 FROM projection_policy_accounts
           WHERE owner_id = NEW.owner_id AND policy_id = NEW.id
       ) THEN
        RAISE EXCEPTION 'all-active policy cannot retain explicit accounts' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER validate_projection_policy_mode
BEFORE UPDATE OF account_selection_mode ON projection_policies
FOR EACH ROW EXECUTE FUNCTION runway_validate_projection_policy_mode();
