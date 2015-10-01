package main_test

import (
	"crypto/tls"
	"fmt"
	"github.com/jackc/pgx"
	"github.com/vaughan0/go-ini"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
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

	cp := &pgx.ConnConfig{}
	cp.Host, _ = file.Get("database", "host")
	if p, ok := file.Get("database", "port"); ok {
		n, err := strconv.ParseUint(p, 10, 16)
		cp.Port = uint16(n)
		if err != nil {
			return nil, err
		}
	}

	cp.Database, _ = file.Get("database", "database")
	cp.User, _ = file.Get("database", "user")
	cp.Password, _ = file.Get("database", "password")

	return cp, nil
}

func tableExists(t *testing.T, tableName string) bool {
	connConfig, err := readConfig("testdata/tern.conf")
	if err != nil {
		t.Fatal(err)
	}

	connConfig.TLSConfig = &tls.Config{InsecureSkipVerify: true}

	conn, err := pgx.Connect(*connConfig)
	if err != nil {
		connConfig.TLSConfig = nil
		conn, err = pgx.Connect(*connConfig)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var exists bool
	err = conn.QueryRow(
		"select exists(select 1 from information_schema.tables where table_catalog=$1 and table_name=$2)",
		connConfig.Database,
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
		{[]string{"-d", "0"}, []string{}, []string{"t1", "t2"}, 0},
		{[]string{}, []string{"t1", "t2"}, []string{}, 2},
		{[]string{"-d", "1"}, []string{"t1"}, []string{"t2"}, 1},
		{[]string{"-d", "-1"}, []string{}, []string{"t1", "t2"}, 0},
		{[]string{"-d", "+1"}, []string{"t1"}, []string{"t2"}, 1},
		{[]string{"-d", "+1"}, []string{"t1", "t2"}, []string{}, 2},
		{[]string{"-d", "-+1"}, []string{"t1", "t2"}, []string{}, 2},
	}

	for i, tt := range tests {
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
	// Ensure database is in clean state
	tern(t, "migrate", "-m", "testdata", "-c", "testdata/tern.conf", "-d", "0")

	output := tern(t, "status", "-m", "testdata", "-c", "testdata/tern.conf")
	expected := `status:   migration(s) pending
version:  0 of 2`
	if !strings.Contains(output, expected) {
		t.Errorf("Expected status output to contain `%s`, but it didn't. Output:\n%s", expected, output)
	}

	// Up all the way
	tern(t, "migrate", "-m", "testdata", "-c", "testdata/tern.conf")

	output = tern(t, "status", "-m", "testdata", "-c", "testdata/tern.conf")
	expected = `status:   up to date
version:  2 of 2`
	if !strings.Contains(output, expected) {
		t.Errorf("Expected status output to contain `%s`, but it didn't. Output:\n%s", expected, output)
	}

	// Back one
	tern(t, "migrate", "-m", "testdata", "-c", "testdata/tern.conf", "-d", "1")

	output = tern(t, "status", "-m", "testdata", "-c", "testdata/tern.conf")
	expected = `status:   migration(s) pending
version:  1 of 2`
	if !strings.Contains(output, expected) {
		t.Errorf("Expected status output to contain `%s`, but it didn't. Output:\n%s", expected, output)
	}
}
