package sqlsplit_test

import (
	"testing"

	"github.com/jackc/tern/migrate/internal/sqlsplit"
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
	} {
		actual := sqlsplit.Split(tt.sql)
		assert.Equalf(t, tt.expected, actual, "%d", i)
	}
}
