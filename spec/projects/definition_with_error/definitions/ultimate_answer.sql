INVALID SQL;

CREATE FUNCTION ultimate_answer() RETURNS integer AS $$
  SELECT 42;
$$ LANGUAGE SQL;

---- CREATE above / DROP below ----

DROP FUNCTION ultimate_answer();
