CREATE TABLE widgets(
  id serial PRIMARY KEY,
  name varchar NOT NULL,
  weight integer NOT NULL
);

---- CREATE above / DROP below ----

DROP TABLE widgets;
