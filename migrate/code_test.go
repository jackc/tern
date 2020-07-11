package migrate_test

import (
	"context"
	"testing"

	"github.com/jackc/tern/migrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadCodePackageSource(t *testing.T) {
	codePackage, err := migrate.LoadCodePackageSource("testdata/code")
	assert.NoError(t, err)
	assert.NotNil(t, codePackage)
}

func TestLoadCodePackageSourceNotCodePackage(t *testing.T) {
	codePackage, err := migrate.LoadCodePackageSource("testdata/sample")
	assert.EqualError(t, err, "unable to load manifest: open testdata/sample/manifest.conf: no such file or directory")
	assert.Nil(t, codePackage)
}
func TestCodePackageInstall(t *testing.T) {
	codePackageSource, err := migrate.LoadCodePackageSource("testdata/code")
	require.NoError(t, err)
	require.NotNil(t, codePackageSource)
	codePackage, err := codePackageSource.Compile()
	require.NoError(t, err)

	conn := connectConn(t)
	defer conn.Close(context.Background())

	err = codePackage.Install(context.Background(), conn, map[string]interface{}{"magic_number": 42})
	require.NoError(t, err)

	var n int
	err = conn.QueryRow(context.Background(), "select code.add(1,2)").Scan(&n)
	require.NoError(t, err)
	assert.Equal(t, 3, n)

	err = conn.QueryRow(context.Background(), "select code.magic_number()").Scan(&n)
	require.NoError(t, err)
	assert.Equal(t, 42, n)
}
