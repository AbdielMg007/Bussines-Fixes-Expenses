CREATE TRIGGER credit_card_cycles_are_immutable
BEFORE UPDATE OR DELETE ON credit_card_cycles
FOR EACH ROW EXECUTE FUNCTION runway_prevent_row_mutation();
