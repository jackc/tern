package sqlsplit_test

import (
	"testing"

	"github.com/jackc/tern/v2/migrate/internal/sqlsplit"
	"github.com/stretchr/testify/assert"
)

func TestSplit(t *testing.T) {
	for i, tt := range []struct {
		sql      string
		expected []string
	}{
		{
			sql:      ``,
			expected: []string{``},
		},
		{
			sql:      `select 42`,
			expected: []string{`select 42`},
		},
		{
			sql:      `select $1`,
			expected: []string{`select $1`},
		},
		{
			sql:      `select 42; select 7;`,
			expected: []string{`select 42;`, `select 7;`},
		},
		{
			sql:      `select 42; select 7`,
			expected: []string{`select 42;`, `select 7`},
		},
		{
			sql:      `select 42, ';' ";"; select 7;`,
			expected: []string{`select 42, ';' ";";`, `select 7;`},
		},
		{
			sql: `select * -- foo ; bar
from users -- ; single line comments
where id = $1;
select 1;
`,
			expected: []string{`select * -- foo ; bar
from users -- ; single line comments
where id = $1;`,
				`select 1;`},
		},
		{
			sql: `select * /* ; multi line ;
;
*/
/* /* ; with nesting ; */ */
from users
where id = $1;
select 1;
`,
			expected: []string{`select * /* ; multi line ;
;
*/
/* /* ; with nesting ; */ */
from users
where id = $1;`,
				`select 1;`},
		},
		{
			sql: `select 1;

DO $$DECLARE r record;
BEGIN
   FOR r IN SELECT table_schema, table_name FROM information_schema.tables
			WHERE table_type = 'VIEW' AND table_schema = 'public'
   LOOP
	   EXECUTE 'GRANT ALL ON ' || quote_ident(r.table_schema) || '.' || quote_ident(r.table_name) || ' TO webuser';
   END LOOP;
END$$;

select 2;
`,
			expected: []string{`select 1;`,
				`DO $$DECLARE r record;
BEGIN
   FOR r IN SELECT table_schema, table_name FROM information_schema.tables
			WHERE table_type = 'VIEW' AND table_schema = 'public'
   LOOP
	   EXECUTE 'GRANT ALL ON ' || quote_ident(r.table_schema) || '.' || quote_ident(r.table_name) || ' TO webuser';
   END LOOP;
END$$;`,
				`select 2;`},
		},
		{
			sql: `select 1;
SELECT $_testął123$hello; world$_testął123$;
select 2;`,
			expected: []string{`select 1;`,
				`SELECT $_testął123$hello; world$_testął123$;`,
				`select 2;`},
		},
		{
			sql: `select 1;
SELECT $test`,
			expected: []string{`select 1;`,
				`SELECT $test`},
		},
		{
			sql: `select 1;
SELECT $test$ending`,
			expected: []string{`select 1;`,
				`SELECT $test$ending`},
		},
		{
			sql: `select 1;
SELECT $test$ending$test`,
			expected: []string{`select 1;`,
				`SELECT $test$ending$test`},
		},
		{
			sql: `select 1;
SELECT $test$ (select $$nested$$ || $testing$strings;$testing$) $test$;
select 2;`,
			expected: []string{`select 1;`,
				`SELECT $test$ (select $$nested$$ || $testing$strings;$testing$) $test$;`,
				`select 2;`},
		},
	} {
		actual := sqlsplit.Split(tt.sql)
		assert.Equalf(t, tt.expected, actual, "%d", i)
	}
}
