create table {{.prefix}}_config(
  id serial primary key
);

---- create above / drop below ----

drop table {{.prefix}}_config;
