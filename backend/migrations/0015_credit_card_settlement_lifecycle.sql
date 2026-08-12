-- Issue 13 final integrity repair. Settlement rows are immutable history; this
-- deferred check verifies their aggregate relationship to the current intent and
-- its one remaining scheduled card flow without manufacturing any payment.
CREATE OR REPLACE FUNCTION runway_validate_card_intent_settlement()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE i RECORD; t RECORD; total BIGINT;
BEGIN
  SELECT * INTO i FROM credit_card_payment_intents
  WHERE id=NEW.intent_id AND owner_id=NEW.owner_id AND account_id=NEW.account_id FOR UPDATE;
  IF NOT FOUND OR i.cycle_id <> NEW.cycle_id OR i.status NOT IN ('active','needs_review') THEN
    RAISE EXCEPTION 'invalid payment intent settlement' USING ERRCODE='23514';
  END IF;
  SELECT lt.amount_minor,lt.currency,lt.destination_account_id,src.id AS source_transaction_id,src.effect,sa.account_type
    INTO t FROM linked_transfers lt
    JOIN financial_transactions src ON src.owner_id=lt.owner_id AND src.transfer_id=lt.id AND src.account_id=lt.source_account_id
    JOIN accounts sa ON sa.owner_id=lt.owner_id AND sa.id=lt.source_account_id
   WHERE lt.owner_id=NEW.owner_id AND lt.id=NEW.transfer_id;
  IF NOT FOUND OR t.destination_account_id <> NEW.account_id OR t.amount_minor <> NEW.amount_minor OR t.currency <> NEW.currency
     OR t.source_transaction_id <> NEW.source_transaction_id OR t.effect <> 'asset_outflow' OR t.account_type NOT IN ('cash','bank') THEN
    RAISE EXCEPTION 'payment intent settlement transfer mismatch' USING ERRCODE='23514';
  END IF;
  SELECT COALESCE(sum(amount_minor),0) INTO total
  FROM credit_card_payment_intent_settlements WHERE intent_id=NEW.intent_id;
  IF total + NEW.amount_minor > i.amount_minor THEN
    RAISE EXCEPTION 'payment intent is over-settled' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION runway_validate_card_intent_flow()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE iid TEXT; i RECORD; flow_count INTEGER; flow_amount BIGINT; flow_date DATE; settled_amount BIGINT;
BEGIN
  IF TG_TABLE_NAME='credit_card_payment_intents' THEN iid := NEW.id;
  ELSIF TG_TABLE_NAME='scheduled_cash_flows' THEN iid := NEW.source_id;
  ELSE iid := NEW.intent_id;
  END IF;
  SELECT * INTO i FROM credit_card_payment_intents WHERE id=iid;
  IF NOT FOUND THEN RETURN NULL; END IF;
  SELECT count(*), COALESCE(max(amount_minor),0), max(financial_date)
    INTO flow_count,flow_amount,flow_date
  FROM scheduled_cash_flows
  WHERE owner_id=i.owner_id AND source_kind='credit_card_payment_intent' AND source_id=i.id AND status='scheduled';
  SELECT COALESCE(sum(amount_minor),0) INTO settled_amount
  FROM credit_card_payment_intent_settlements WHERE intent_id=i.id;
  IF settled_amount > i.amount_minor THEN
    RAISE EXCEPTION 'payment intent is over-settled' USING ERRCODE='23514';
  END IF;
  IF settled_amount=i.amount_minor THEN
    IF i.status <> 'settled' OR flow_count <> 0 THEN
      RAISE EXCEPTION 'settled payment intent lifecycle is inconsistent' USING ERRCODE='23514';
    END IF;
  ELSIF i.status='active' THEN
    IF flow_count <> 1 OR flow_amount <> i.amount_minor-settled_amount OR flow_date <> i.planned_date THEN
      RAISE EXCEPTION 'active payment intent must have one matching scheduled cash flow' USING ERRCODE='23514';
    END IF;
  ELSIF flow_count <> 0 THEN
    RAISE EXCEPTION 'non-active payment intent cannot have an active cash flow' USING ERRCODE='23514';
  END IF;
  RETURN NULL;
END $$;

CREATE CONSTRAINT TRIGGER validate_card_intent_flow_from_settlement
AFTER INSERT ON credit_card_payment_intent_settlements
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION runway_validate_card_intent_flow();
