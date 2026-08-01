package main

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeConnectionStringSettings(t *testing.T) {
	overrides := map[string]string{
		"PGHOST":     "override.example.com",
		"PGPORT":     "6543",
		"PGDATABASE": "override database",
		"PGUSER":     "override user",
		"PGPASSWORD": "override \\ ' password",
		"PGSSLMODE":  "require",
	}

	tests := []struct {
		name       string
		connString string
	}{
		{
			name:       "keyword value",
			connString: "host=base.example.com port=5432 dbname=base_database user=base_user password=base_password sslmode=disable application_name=tern_test",
		},
		{
			name:       "URL",
			connString: "postgresql://base_user:base_password@base.example.com:5432/base_database?sslmode=disable&application_name=tern_test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connString, err := mergeConnectionStringSettings(tt.connString, overrides)
			require.NoError(t, err)

			config, err := pgx.ParseConfig(connString)
			require.NoError(t, err)

			assert.Equal(t, "override.example.com", config.Host)
			assert.Equal(t, uint16(6543), config.Port)
			assert.Equal(t, "override database", config.Database)
			assert.Equal(t, "override user", config.User)
			assert.Equal(t, "override \\ ' password", config.Password)
			assert.Equal(t, "tern_test", config.RuntimeParams["application_name"])
			assert.NotNil(t, config.TLSConfig)
			assert.Empty(t, config.Fallbacks)
		})
	}
}

func TestMergeConnectionStringSettingsRemovesURLDatabaseAlias(t *testing.T) {
	clearTestPGEnv(t)
	connString, err := mergeConnectionStringSettings(
		"postgresql://baseuser:basepassword@basehost:5432/?database=basedb&sslmode=disable",
		map[string]string{"PGDATABASE": "configdb"},
	)
	require.NoError(t, err)

	u, err := url.Parse(connString)
	require.NoError(t, err)
	assert.Empty(t, u.Query().Get("database"))
	assert.Equal(t, "configdb", u.Query().Get("dbname"))

	// pgx iterates URL query parameters as a map. Repeated parsing guards
	// against a canonical alias collision making the selected database random.
	for range 100 {
		config, err := pgx.ParseConfig(connString)
		require.NoError(t, err)
		assert.Equal(t, "configdb", config.Database)
	}
}

func TestMergeConnectionStringSettingsSkipsEmptyNonPasswordValues(t *testing.T) {
	clearTestPGEnv(t)
	t.Setenv("PGUSER", "ambient_user")

	for _, connString := range []string{
		"dbname=test_database sslmode=disable",
		"postgresql:///test_database?sslmode=disable",
	} {
		merged, err := mergeConnectionStringSettings(connString, map[string]string{
			"PGHOST": "",
			"PGUSER": "",
		})
		require.NoError(t, err)

		config, err := pgx.ParseConfig(merged)
		require.NoError(t, err)
		assert.NotEmpty(t, config.Host)
		assert.Equal(t, "ambient_user", config.User)
	}
}

func TestLoadConfigCLIArgsOverridePGService(t *testing.T) {
	serviceFile := writeTestServiceFile(t, `
[precedence_test]
host=service.example.com
port=5432
dbname=service_database
user=service_user
password=service_password
sslmode=disable
`)
	setTestPGService(t, serviceFile)
	setTestCLIOptions(t, func() {
		cliOptions.host = "cli.example.com"
		cliOptions.port = 6543
		cliOptions.database = "cli_database"
		cliOptions.user = "cli_user"
		cliOptions.password = "cli_password"
		cliOptions.sslmode = "require"
	})

	config, err := LoadConfig()
	require.NoError(t, err)

	assert.Equal(t, "cli.example.com", config.ConnConfig.Host)
	assert.Equal(t, uint16(6543), config.ConnConfig.Port)
	assert.Equal(t, "cli_database", config.ConnConfig.Database)
	assert.Equal(t, "cli_user", config.ConnConfig.User)
	assert.Equal(t, "cli_password", config.ConnConfig.Password)
	assert.NotNil(t, config.ConnConfig.TLSConfig)
	assert.Empty(t, config.ConnConfig.Fallbacks)
}

func TestLoadConfigCLIUserSelectsMatchingPassfileEntry(t *testing.T) {
	serviceFile := writeTestServiceFile(t, `
[precedence_test]
host=service.example.com
port=5432
dbname=service_database
user=service_user
sslmode=disable
`)
	setTestPGService(t, serviceFile)

	passfile := filepath.Join(t.TempDir(), "pgpass")
	err := os.WriteFile(passfile, []byte(
		"service.example.com:5432:service_database:service_user:service_password\n"+
			"service.example.com:5432:service_database:cli_user:cli_password\n",
	), 0o600)
	require.NoError(t, err)
	t.Setenv("PGPASSFILE", passfile)

	setTestCLIOptions(t, func() {
		cliOptions.user = "cli_user"
	})

	config, err := LoadConfig()
	require.NoError(t, err)

	assert.Equal(t, "cli_user", config.ConnConfig.User)
	assert.Equal(t, "cli_password", config.ConnConfig.Password)
}

func TestLoadConfigCLIConnStringOverridesConfigFileSettings(t *testing.T) {
	clearTestPGEnv(t)
	configFile := writeTestConfigFile(t, `
[database]
host=confighost
port=5432
database=configdb
user=configuser
password=configpassword
sslmode=disable
`)
	setTestCLIOptions(t, func() {
		cliOptions.configPaths = []string{configFile}
		cliOptions.connString = "postgresql://cliuser:clipassword@clihost:6000/clidb?sslmode=require"
	})

	config, err := LoadConfig()
	require.NoError(t, err)

	assert.Equal(t, "clihost", config.ConnConfig.Host)
	assert.Equal(t, uint16(6000), config.ConnConfig.Port)
	assert.Equal(t, "clidb", config.ConnConfig.Database)
	assert.Equal(t, "cliuser", config.ConnConfig.User)
	assert.Equal(t, "clipassword", config.ConnConfig.Password)
	assert.NotNil(t, config.ConnConfig.TLSConfig)
}

func TestLoadConfigUpdatesConnStringWithConfigFileOverrides(t *testing.T) {
	clearTestPGEnv(t)
	configFile := writeTestConfigFile(t, `
[database]
conn_string=postgresql://baseuser:basepassword@basehost:5432/basedb?sslmode=disable
host=confighost
database=configdb
`)
	setTestCLIOptions(t, func() {
		cliOptions.configPaths = []string{configFile}
	})

	config, err := LoadConfig()
	require.NoError(t, err)

	printedConfig, err := pgx.ParseConfig(config.ConnString)
	require.NoError(t, err)
	assert.Equal(t, config.ConnConfig.Host, printedConfig.Host)
	assert.Equal(t, config.ConnConfig.Database, printedConfig.Database)
	assert.Equal(t, "confighost", printedConfig.Host)
	assert.Equal(t, "configdb", printedConfig.Database)
}

func TestLoadConfigEmptyPasswordMasksEnvironment(t *testing.T) {
	clearTestPGEnv(t)
	t.Setenv("PGPASSWORD", "ambient_secret")
	configFile := writeTestConfigFile(t, `
[database]
host=confighost
database=configdb
user=configuser
password=
sslmode=disable
`)
	setTestCLIOptions(t, func() {
		cliOptions.configPaths = []string{configFile}
	})

	config, err := LoadConfig()
	require.NoError(t, err)

	assert.Equal(t, "configuser", config.ConnConfig.User)
	assert.Empty(t, config.ConnConfig.Password)
}

func TestLoadConfigEmptyHostFallsBackToDefault(t *testing.T) {
	clearTestPGEnv(t)
	configFile := writeTestConfigFile(t, `
[database]
host=
database=configdb
user=configuser
sslmode=disable
`)
	setTestCLIOptions(t, func() {
		cliOptions.configPaths = []string{configFile}
	})

	config, err := LoadConfig()
	require.NoError(t, err)

	assert.NotEmpty(t, config.ConnConfig.Host)
	assert.Equal(t, "configdb", config.ConnConfig.Database)
	assert.Equal(t, "configuser", config.ConnConfig.User)
}

func writeTestServiceFile(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "pg_service.conf")
	err := os.WriteFile(path, []byte(contents), 0o600)
	require.NoError(t, err)

	return path
}

func writeTestConfigFile(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "tern.conf")
	err := os.WriteFile(path, []byte(contents), 0o600)
	require.NoError(t, err)

	return path
}

func setTestPGService(t *testing.T, serviceFile string) {
	t.Helper()
	clearTestPGEnv(t)
	t.Setenv("PGSERVICE", "precedence_test")
	t.Setenv("PGSERVICEFILE", serviceFile)
}

func clearTestPGEnv(t *testing.T) {
	t.Helper()

	for _, envvar := range []string{
		"PGHOST",
		"PGPORT",
		"PGDATABASE",
		"PGUSER",
		"PGPASSWORD",
		"PGPASSFILE",
		"PGSSLMODE",
		"PGSSLROOTCERT",
		"PGSERVICE",
		"PGSERVICEFILE",
		"TERN_CONFIG",
		"TERN_MIGRATIONS",
	} {
		t.Setenv(envvar, "")
	}
}

func setTestCLIOptions(t *testing.T, set func()) {
	t.Helper()

	previous := cliOptions
	cliOptions = cliOptionsConfig{}
	t.Cleanup(func() {
		cliOptions = previous
	})

	emptyConfig := writeTestConfigFile(t, "[database]\n")
	cliOptions.configPaths = []string{emptyConfig}
	if cliOptions.migrationsPath == "" {
		cliOptions.migrationsPath = t.TempDir()
	}
	set()
}
