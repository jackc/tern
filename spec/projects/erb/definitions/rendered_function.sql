CREATE FUNCTION rendered_function() RETURNS integer AS $$
  SELECT <%= @num_from_outer_template %>
$$ LANGUAGE SQL;

---- CREATE above / DROP below ----

DROP FUNCTION rendered_function();
