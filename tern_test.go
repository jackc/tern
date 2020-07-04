package main_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vaughan0/go-ini"
)

func TestMain(m *testing.M) {
	err := exec.Command("go", "build", "-o", "tmp/tern").Run()
	if err != nil {
		fmt.Println("Failed to build tern binary:", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

func readConfig(path string) (*pgx.ConnConfig, error) {
	file, err := ini.LoadFile(path)
	if err != nil {
		return nil, err
	}

	cp, _ := pgx.ParseConfig("")
	if s, ok := file.Get("database", "host"); ok {
		cp.Host = s
	}
	if p, ok := file.Get("database", "port"); ok {
		n, err := strconv.ParseUint(p, 10, 16)
		cp.Port = uint16(n)
		if err != nil {
			return nil, err
		}
	}

	if s, ok := file.Get("database", "database"); ok {
		cp.Database = s
	}
	if s, ok := file.Get("database", "user"); ok {
		cp.User = s
	}
	if s, ok := file.Get("database", "Password"); ok {
		cp.Password = s
	}

	return cp, nil
}

func connectConn(t *testing.T) *pgx.Conn {
	ctx := context.Background()
	connConfig, err := readConfig("testdata/tern.conf")
	require.NoError(t, err)

	connConfig.TLSConfig = &tls.Config{InsecureSkipVerify: true}

	conn, err := pgx.ConnectConfig(ctx, connConfig)
	if err != nil {
		connConfig.TLSConfig = nil
		conn, err = pgx.ConnectConfig(ctx, connConfig)
	}
	require.NoError(t, err)

	return conn
}

func tableExists(t *testing.T, tableName string) bool {
	ctx := context.Background()
	conn := connectConn(t)
	defer conn.Close(ctx)

	var exists bool
	err := conn.QueryRow(
		ctx,
		"select exists(select 1 from information_schema.tables where table_catalog=current_database() and table_name=$1)",
		tableName,
	).Scan(&exists)
	if err != nil {
		t.Fatal(err)
	}

	return exists
}

func currentVersion(t *testing.T) int32 {
	output := tern(t, "status", "-m", "testdata", "-c", "testdata/tern.conf")
	re := regexp.MustCompile(`version:\s+(\d+)`)
	match := re.FindStringSubmatch(output)
	if match == nil {
		t.Fatalf("could not extract current version from status:\n%s", output)
	}

	n, err := strconv.ParseInt(match[1], 10, 32)
	if err != nil {
		t.Fatal(err)
	}

	return int32(n)
}

func tern(t *testing.T, args ...string) string {
	cmd := exec.Command("tmp/tern", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tern failed with: %v\noutput:\n%v", err, string(output))
	}

	return string(output)
}

func prepareDatabase(t *testing.T) {
	testDatabase, _ := os.LookupEnv("TERN_TEST_DATABASE")
	require.NotEqualf(t, "", testDatabase, "TERN_TEST_DATABASE must be set")

	cmd := exec.Command("dropdb", testDatabase)
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "dropdb failed with output: %s", string(output))

	cmd = exec.Command("createdb", testDatabase)
	output, err = cmd.CombinedOutput()
	require.NoErrorf(t, err, "createdb failed with output: %s", string(output))
}

func TestInitWithoutDirectory(t *testing.T) {
	defer func() {
		os.Remove("tern.conf")
		os.Remove("001_create_people.sql.example")
	}()

	tern(t, "init")

	expectedFiles := []string{"tern.conf", "001_create_people.sql.example"}
	for _, f := range expectedFiles {
		_, err := os.Stat(f)
		if err != nil {
			t.Errorf(`Expected init to create "%s", but it didn't: %v`, f, err)
		}
	}
}

func TestInitWithDirectory(t *testing.T) {
	defer func() {
		os.RemoveAll("tmp/init")
	}()

	tern(t, "init", "tmp/init")

	expectedFiles := []string{"tmp/init/tern.conf", "tmp/init/001_create_people.sql.example"}
	for _, f := range expectedFiles {
		_, err := os.Stat(f)
		if err != nil {
			t.Errorf(`Expected init to create "%s", but it didn't: %v`, f, err)
		}
	}
}

func TestNew(t *testing.T) {
	path := "tmp/new"
	defer func() {
		os.RemoveAll(path)
	}()

	tern(t, "init", path)
	tern(t, "new", "-m", path, "first")
	tern(t, "new", "-m", path, "second")

	expectedFiles := []string{"tmp/new/001_first.sql", "tmp/new/002_second.sql"}
	for _, f := range expectedFiles {
		_, err := os.Stat(f)
		if err != nil {
			t.Errorf(`Expected init to create "%s", but it didn't: %v`, f, err)
		}
	}
}

func TestMigrate(t *testing.T) {
	tests := []struct {
		args              []string
		expectedExists    []string
		expectedNotExists []string
		expectedVersion   int32
	}{
		{[]string{"-d", "1"}, []string{"t1"}, []string{"t2"}, 1},
		{[]string{"-d", "2"}, []string{"t1", "t2"}, []string{}, 2},
	}

	for i, tt := range tests {
		prepareDatabase(t)

		baseArgs := []string{"migrate", "-m", "testdata", "-c", "testdata/tern.conf"}
		args := append(baseArgs, tt.args...)

		tern(t, args...)

		for _, tableName := range tt.expectedExists {
			if !tableExists(t, tableName) {
				t.Fatalf("%d. Expected table %s to exist, but it doesn't", i, tableName)
			}
		}

		for _, tableName := range tt.expectedNotExists {
			if tableExists(t, tableName) {
				t.Fatalf("%d. Expected table %s to not exist, but it does", i, tableName)
			}
		}

		if currentVersion(t) != tt.expectedVersion {
			t.Fatalf(`Expected current version to be %d, but it was %d`, tt.expectedVersion, currentVersion(t))
		}
	}
}

func TestStatus(t *testing.T) {
	prepareDatabase(t)

	output := tern(t, "status", "-m", "testdata", "-c", "testdata/tern.conf")
	expected := `status:   migration(s) pending
version:  0 of 2`
	require.Contains(t, output, expected)

	tern(t, "migrate", "-m", "testdata", "-c", "testdata/tern.conf", "-d", "1")

	output = tern(t, "status", "-m", "testdata", "-c", "testdata/tern.conf")
	expected = `status:   migration(s) pending
version:  1 of 2`
	require.Contains(t, output, expected)

	tern(t, "migrate", "-m", "testdata", "-c", "testdata/tern.conf")

	output = tern(t, "status", "-m", "testdata", "-c", "testdata/tern.conf")
	expected = `status:   up to date
version:  2 of 2`
	require.Contains(t, output, expected)
}

func TestInstallCode(t *testing.T) {
	prepareDatabase(t)

	tern(t, "code", "install", "-c", "testdata/tern.conf", "testdata/code")

	conn := connectConn(t)
	defer conn.Close(context.Background())

	var n int
	err := conn.QueryRow(context.Background(), "select add(1,2)").Scan(&n)
	require.NoError(t, err)
	assert.Equal(t, 3, n)
}

func TestCLIArgsWithoutConfigFile(t *testing.T) {
	prepareDatabase(t)

	connConfig, err := readConfig("testdata/tern.conf")
	if err != nil {
		t.Fatal(err)
	}

	output := tern(t, "status",
		"-m", "testdata",
		"--host", connConfig.Host,
		"--port", strconv.FormatInt(int64(connConfig.Port), 10),
		"--user", connConfig.User,
		"--password", connConfig.Password,
		"--database", connConfig.Database,
	)
	expected := `status:   migration(s) pending
version:  0 of 2`
	if !strings.Contains(output, expected) {
		t.Errorf("Expected status output to contain `%s`, but it didn't. Output:\n%s", expected, output)
	}
}

func TestConfigFileTemplateEvalWithEnvVar(t *testing.T) {
	prepareDatabase(t)

	connConfig, err := readConfig("testdata/tern.conf")
	if err != nil {
		t.Fatal(err)
	}

	err = os.Setenv("TERNHOST", connConfig.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := os.Unsetenv("TERNHOST")
		if err != nil {
			t.Fatal(err)
		}
	}()

	output := tern(t, "status",
		"-c", "testdata/tern-envvar-deprecated.conf",
		"-m", "testdata",
		"--port", strconv.FormatInt(int64(connConfig.Port), 10),
		"--user", connConfig.User,
		"--password", connConfig.Password,
		"--database", connConfig.Database,
	)
	expected := `status:   migration(s) pending
version:  0 of 2`
	if !strings.Contains(output, expected) {
		t.Errorf("Expected status output to contain `%s`, but it didn't. Output:\n%s", expected, output)
	}
}

func TestConfigFileTemplateEvalWithDeprecatedEnvVar(t *testing.T) {
	prepareDatabase(t)

	connConfig, err := readConfig("testdata/tern.conf")
	if err != nil {
		t.Fatal(err)
	}

	err = os.Setenv("TERNHOST", connConfig.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := os.Unsetenv("TERNHOST")
		if err != nil {
			t.Fatal(err)
		}
	}()

	output := tern(t, "status",
		"-c", "testdata/tern-envvar-deprecated.conf",
		"-m", "testdata",
		"--port", strconv.FormatInt(int64(connConfig.Port), 10),
		"--user", connConfig.User,
		"--password", connConfig.Password,
		"--database", connConfig.Database,
	)
	expected := `status:   migration(s) pending
version:  0 of 2`
	if !strings.Contains(output, expected) {
		t.Errorf("Expected status output to contain `%s`, but it didn't. Output:\n%s", expected, output)
	}
}

func TestSSHTunnel(t *testing.T) {
	host := os.Getenv("TERN_HOST")
	if host == "" {
		t.Skip("Skipping SSH Tunnel test due to missing TERN_HOST environment variable")
	}

	user := os.Getenv("TERN_USER")
	if user == "" {
		t.Skip("Skipping SSH Tunnel test due to missing TERN_USER environment variable")
	}

	password := os.Getenv("TERN_PASSWORD")
	if password == "" {
		t.Skip("Skipping SSH Tunnel test due to missing TERN_PASSWORD environment variable")
	}

	database := os.Getenv("TERN_DATABASE")
	if database == "" {
		t.Skip("Skipping SSH Tunnel test due to missing TERN_DATABASE environment variable")
	}

	// Ensure database is in clean state
	tern(t, "migrate", "-m", "testdata", "-c", "testdata/tern.conf", "-d", "0")

	output := tern(t, "status",
		"-m", "testdata",
		"--ssh-host", "localhost",
		"--host", host,
		"--user", user,
		"--password", password,
		"--database", database,
	)
	expected := `status:   migration(s) pending
version:  0 of 2`
	if !strings.Contains(output, expected) {
		t.Errorf("Expected status output to contain `%s`, but it didn't. Output:\n%s", expected, output)
	}
}
