CREATE FUNCTION ultimate_answer() RETURNS integer AS $$
  SELECT <%= 7*6 %>
$$ LANGUAGE SQL;

---- CREATE above / DROP below ----

DROP FUNCTION ultimate_answer();
