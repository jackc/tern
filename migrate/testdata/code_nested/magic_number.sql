drop function if exists magic_number();

create function magic_number() returns int
language plpgsql as $$
begin
  return {{.magic_number}};
end;
$$;
