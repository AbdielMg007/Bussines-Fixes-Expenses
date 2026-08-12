-- Keep already-migrated development databases aligned with the Issue 13
-- card-flow lifecycle trigger. No financial rows are rewritten.
CREATE OR REPLACE FUNCTION runway_validate_card_intent_flow() RETURNS TRIGGER LANGUAGE plpgsql AS $$
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
