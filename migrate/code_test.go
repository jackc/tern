package migrate_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/tern/v2/migrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadCodePackage(t *testing.T) {
	codePackage, err := migrate.LoadCodePackage(os.DirFS("testdata/code"))
	assert.NoError(t, err)
	assert.NotNil(t, codePackage)
}

func TestLoadCodePackageNotCodePackage(t *testing.T) {
	codePackage, err := migrate.LoadCodePackage(os.DirFS("testdata/sample"))
	assert.EqualError(t, err, "install.sql not found")
	assert.Nil(t, codePackage)
}
func TestInstallCodePackage(t *testing.T) {
	codePackage, err := migrate.LoadCodePackage(os.DirFS("testdata/code"))
	require.NoError(t, err)
	require.NotNil(t, codePackage)

	conn := connectConn(t)
	defer conn.Close(context.Background())

	err = migrate.InstallCodePackage(context.Background(), conn, map[string]interface{}{"magic_number": 42}, codePackage)
	require.NoError(t, err)

	var n int
	err = conn.QueryRow(context.Background(), "select add(1,2)").Scan(&n)
	require.NoError(t, err)
	assert.Equal(t, 3, n)

	err = conn.QueryRow(context.Background(), "select magic_number()").Scan(&n)
	require.NoError(t, err)
	assert.Equal(t, 42, n)
}
