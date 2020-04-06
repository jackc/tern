drop function if exists add(int, int);

create function add(int, int) returns int
language plpgsql as $$
begin
  return $1 + $2;
end;
$$;
