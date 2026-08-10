CREATE TABLE accounts (
    id TEXT PRIMARY KEY CHECK (length(btrim(id)) > 0),
    owner_id TEXT NOT NULL REFERENCES owners(id) ON DELETE RESTRICT,
    display_name TEXT NOT NULL CHECK (length(btrim(display_name)) BETWEEN 1 AND 120),
    account_type TEXT NOT NULL CHECK (account_type IN ('cash', 'bank', 'credit_card', 'loan')),
    currency TEXT NOT NULL CHECK (currency = 'MXN'),
    status TEXT NOT NULL CHECK (status IN ('active', 'archived')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (owner_id, id),
    CHECK (updated_at >= created_at)
);

CREATE INDEX accounts_owner_created_idx ON accounts (owner_id, created_at, id);

CREATE TABLE linked_transfers (
    id TEXT PRIMARY KEY CHECK (length(btrim(id)) > 0),
    owner_id TEXT NOT NULL REFERENCES owners(id) ON DELETE RESTRICT,
    source_account_id TEXT NOT NULL,
    destination_account_id TEXT NOT NULL,
    amount_minor BIGINT NOT NULL CHECK (amount_minor > 0),
    currency TEXT NOT NULL CHECK (currency = 'MXN'),
    financial_date DATE NOT NULL,
    memo TEXT NOT NULL DEFAULT '' CHECK (char_length(memo) <= 500),
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (owner_id, id),
    FOREIGN KEY (owner_id, source_account_id) REFERENCES accounts(owner_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (owner_id, destination_account_id) REFERENCES accounts(owner_id, id) ON DELETE RESTRICT,
    CHECK (source_account_id <> destination_account_id),
    CHECK (financial_date BETWEEN DATE '0001-01-01' AND DATE '9999-12-31')
);

CREATE TABLE financial_transactions (
    ledger_sequence BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    id TEXT NOT NULL UNIQUE CHECK (length(btrim(id)) > 0),
    owner_id TEXT NOT NULL,
    account_id TEXT NOT NULL,
    transaction_kind TEXT NOT NULL CHECK (transaction_kind IN ('manual', 'transfer')),
    amount_minor BIGINT NOT NULL CHECK (amount_minor > 0),
    currency TEXT NOT NULL CHECK (currency = 'MXN'),
    effect TEXT NOT NULL CHECK (effect IN ('asset_inflow', 'asset_outflow', 'liability_charge', 'liability_payment')),
    financial_date DATE NOT NULL,
    memo TEXT NOT NULL DEFAULT '' CHECK (char_length(memo) <= 500),
    transfer_id TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (owner_id, id),
    UNIQUE (transfer_id, account_id),
    FOREIGN KEY (owner_id, account_id) REFERENCES accounts(owner_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (owner_id, transfer_id) REFERENCES linked_transfers(owner_id, id) ON DELETE RESTRICT,
    CHECK (
        (transaction_kind = 'manual' AND transfer_id IS NULL)
        OR (transaction_kind = 'transfer' AND transfer_id IS NOT NULL)
    ),
    CHECK (financial_date BETWEEN DATE '0001-01-01' AND DATE '9999-12-31')
);

CREATE INDEX financial_transactions_account_sequence_idx
    ON financial_transactions (owner_id, account_id, ledger_sequence);
CREATE INDEX financial_transactions_transfer_idx
    ON financial_transactions (owner_id, transfer_id)
    WHERE transfer_id IS NOT NULL;

CREATE TABLE balance_snapshots (
    id TEXT PRIMARY KEY CHECK (length(btrim(id)) > 0),
    snapshot_sequence BIGINT GENERATED ALWAYS AS IDENTITY UNIQUE,
    owner_id TEXT NOT NULL,
    account_id TEXT NOT NULL,
    balance_minor BIGINT NOT NULL,
    currency TEXT NOT NULL CHECK (currency = 'MXN'),
    effective_at TIMESTAMPTZ NOT NULL,
    cutoff_sequence BIGINT NOT NULL CHECK (cutoff_sequence >= 0),
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (owner_id, id),
    FOREIGN KEY (owner_id, account_id) REFERENCES accounts(owner_id, id) ON DELETE RESTRICT
);

CREATE INDEX balance_snapshots_latest_idx
    ON balance_snapshots (owner_id, account_id, cutoff_sequence DESC, snapshot_sequence DESC);
