-- Issue 13: the only persisted non-manual scheduled flow is the remaining
-- cash amount of an active credit-card PaymentIntent.
ALTER TABLE financial_mutations DROP CONSTRAINT financial_mutations_operation_check;
ALTER TABLE financial_mutations ADD CONSTRAINT financial_mutations_operation_check CHECK (operation IN (
  'post_transaction','create_snapshot','create_transfer','create_obligation','archive_obligation',
  'create_scheduled_flow','cancel_scheduled_flow','create_receivable','collect_receivable','cancel_receivable',
  'register_credit_card_statement','create_installment_plan','record_installment_principal_payment',
  'settle_credit_card_payment_intent'
));

ALTER TABLE scheduled_cash_flows DROP CONSTRAINT scheduled_cash_flows_source_kind_check;
ALTER TABLE scheduled_cash_flows ADD CONSTRAINT scheduled_cash_flows_source_kind_check CHECK (
  source_kind IN ('manual_expected_income','manual_other','credit_card_payment_intent')
);
ALTER TABLE credit_card_payment_intents DROP CONSTRAINT credit_card_payment_intents_status_check;
ALTER TABLE credit_card_payment_intents ADD CONSTRAINT credit_card_payment_intents_status_check CHECK (
  status IN ('active','needs_review','cancelled','settled')
);
ALTER TABLE credit_card_payment_intents ADD CONSTRAINT credit_card_payment_intents_owner_account_id_key UNIQUE(owner_id,account_id,id);

CREATE UNIQUE INDEX one_scheduled_card_flow_per_intent
  ON scheduled_cash_flows(owner_id,source_id)
  WHERE source_kind='credit_card_payment_intent' AND status='scheduled';

CREATE TABLE credit_card_payment_intent_settlements (
  id TEXT PRIMARY KEY CHECK (length(btrim(id)) > 0),
  owner_id TEXT NOT NULL,
  account_id TEXT NOT NULL,
  cycle_id TEXT NOT NULL,
  intent_id TEXT NOT NULL,
  transfer_id TEXT NOT NULL,
  amount_minor BIGINT NOT NULL CHECK(amount_minor > 0),
  currency TEXT NOT NULL CHECK(currency='MXN'),
  source_transaction_id TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  UNIQUE(owner_id,id),
  UNIQUE(owner_id,transfer_id),
  FOREIGN KEY(owner_id,account_id,intent_id) REFERENCES credit_card_payment_intents(owner_id,account_id,id) ON DELETE RESTRICT,
  FOREIGN KEY(owner_id,account_id,cycle_id) REFERENCES credit_card_cycles(owner_id,account_id,id) ON DELETE RESTRICT,
  FOREIGN KEY(owner_id,transfer_id) REFERENCES linked_transfers(owner_id,id) ON DELETE RESTRICT,
  FOREIGN KEY(owner_id,source_transaction_id) REFERENCES financial_transactions(owner_id,id) ON DELETE RESTRICT
);

CREATE FUNCTION runway_validate_card_scheduled_flow_source() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.source_kind='credit_card_payment_intent' THEN
    IF NEW.direction <> 'outflow' OR NEW.amount_provenance <> 'exact' OR NEW.date_provenance <> 'exact' OR NEW.inclusion_eligibility <> 'eligible' THEN
      RAISE EXCEPTION 'card payment flow has invalid semantics' USING ERRCODE='23514';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM credit_card_payment_intents i WHERE i.owner_id=NEW.owner_id AND i.id=NEW.source_id AND i.status='active') THEN
      RAISE EXCEPTION 'card payment flow intent mismatch' USING ERRCODE='23503';
    END IF;
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER validate_card_scheduled_flow_source BEFORE INSERT ON scheduled_cash_flows
FOR EACH ROW EXECUTE FUNCTION runway_validate_card_scheduled_flow_source();

CREATE FUNCTION runway_validate_card_intent_flow() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE iid TEXT; i RECORD; flow_count INTEGER; flow_amount BIGINT; flow_date DATE; settled_amount BIGINT;
BEGIN
  IF TG_TABLE_NAME='credit_card_payment_intents' THEN
    iid := NEW.id;
  ELSE
    iid := NEW.source_id;
  END IF;
  SELECT * INTO i FROM credit_card_payment_intents WHERE id=iid;
  IF NOT FOUND THEN RETURN NULL; END IF;
  SELECT count(*), COALESCE(max(amount_minor),0), max(financial_date) INTO flow_count,flow_amount,flow_date
  FROM scheduled_cash_flows WHERE owner_id=i.owner_id AND source_kind='credit_card_payment_intent' AND source_id=i.id AND status='scheduled';
  SELECT COALESCE(sum(amount_minor),0) INTO settled_amount FROM credit_card_payment_intent_settlements WHERE intent_id=i.id;
  IF i.status='active' THEN
    IF flow_count <> 1 OR flow_amount <> i.amount_minor-settled_amount OR flow_date <> i.planned_date OR settled_amount >= i.amount_minor THEN
      RAISE EXCEPTION 'active payment intent must have one matching scheduled cash flow' USING ERRCODE='23514';
    END IF;
  ELSIF flow_count <> 0 THEN
    RAISE EXCEPTION 'non-active payment intent cannot have an active cash flow' USING ERRCODE='23514';
  END IF;
  IF (i.status='settled' AND settled_amount <> i.amount_minor) OR (i.status <> 'settled' AND settled_amount = i.amount_minor) THEN
    RAISE EXCEPTION 'payment intent settlement lifecycle is inconsistent' USING ERRCODE='23514';
  END IF;
  RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER validate_card_intent_flow_from_intent AFTER INSERT OR UPDATE ON credit_card_payment_intents
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION runway_validate_card_intent_flow();
CREATE CONSTRAINT TRIGGER validate_card_intent_flow_from_flow AFTER INSERT OR UPDATE ON scheduled_cash_flows
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.source_kind='credit_card_payment_intent') EXECUTE FUNCTION runway_validate_card_intent_flow();

CREATE FUNCTION runway_validate_card_intent_settlement() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE i RECORD; t RECORD; total BIGINT;
BEGIN
  SELECT * INTO i FROM credit_card_payment_intents WHERE id=NEW.intent_id AND owner_id=NEW.owner_id AND account_id=NEW.account_id FOR UPDATE;
  IF NOT FOUND OR i.cycle_id <> NEW.cycle_id OR i.status NOT IN ('active','needs_review') THEN
    RAISE EXCEPTION 'invalid payment intent settlement' USING ERRCODE='23514';
  END IF;
  SELECT lt.amount_minor,lt.currency,lt.source_account_id,lt.destination_account_id,src.id AS source_transaction_id,src.effect,src.account_id AS source_transaction_account,sa.account_type
    INTO t FROM linked_transfers lt
    JOIN financial_transactions src ON src.owner_id=lt.owner_id AND src.transfer_id=lt.id AND src.account_id=lt.source_account_id
    JOIN accounts sa ON sa.owner_id=lt.owner_id AND sa.id=lt.source_account_id
   WHERE lt.owner_id=NEW.owner_id AND lt.id=NEW.transfer_id;
  IF NOT FOUND OR t.destination_account_id <> NEW.account_id OR t.amount_minor <> NEW.amount_minor OR t.currency <> NEW.currency
     OR t.source_transaction_id <> NEW.source_transaction_id OR t.effect <> 'asset_outflow' OR t.account_type NOT IN ('cash','bank') THEN
    RAISE EXCEPTION 'payment intent settlement transfer mismatch' USING ERRCODE='23514';
  END IF;
  SELECT COALESCE(sum(amount_minor),0) INTO total FROM credit_card_payment_intent_settlements WHERE intent_id=NEW.intent_id;
  IF total > i.amount_minor THEN RAISE EXCEPTION 'payment intent is over-settled' USING ERRCODE='23514'; END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER validate_card_intent_settlement BEFORE INSERT ON credit_card_payment_intent_settlements
FOR EACH ROW EXECUTE FUNCTION runway_validate_card_intent_settlement();
CREATE TRIGGER card_intent_settlements_immutable BEFORE UPDATE OR DELETE ON credit_card_payment_intent_settlements
FOR EACH ROW EXECUTE FUNCTION runway_prevent_row_mutation();
