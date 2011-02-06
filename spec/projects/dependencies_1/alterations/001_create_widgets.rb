Sequel.migration do
  up do
    run <<-END_SQL
      CREATE TABLE widgets(
        id serial PRIMARY KEY,
        name varchar NOT NULL,
        weight integer NOT NULL
      );
    END_SQL
  end

  down do
    run <<-END_SQL
      DROP TABLE widgets;
    END_SQL
  end
end
