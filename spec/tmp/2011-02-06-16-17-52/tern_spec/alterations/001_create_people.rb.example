Sequel.migration do
  up do
    run <<-END_SQL
      CREATE TABLE people(
        id serial PRIMARY KEY,
        name varchar NOT NULL
      );
    END_SQL
  end

  down do
    run <<-END_SQL
      DROP TABLE people;
    END_SQL
  end
end
