package main_test

import (
	"github.com/JackC/pgx"
	"github.com/vaughan0/go-ini"
	. "gopkg.in/check.v1"
	"os"
	"os/exec"
	"strconv"
	"testing"
)

type TernSuite struct {
	conn       *pgx.Connection
	connParams *pgx.ConnectionParameters
}

func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&TernSuite{})

func (s *TernSuite) SetUpSuite(c *C) {
	err := exec.Command("go", "build", "-o", "tmp/tern").Run()
	c.Assert(err, IsNil)

	s.connParams, err = readConfig("testdata/tern.conf")
	c.Assert(err, IsNil)

	s.conn, err = pgx.Connect(*s.connParams)
	c.Assert(err, IsNil)
}

func readConfig(path string) (*pgx.ConnectionParameters, error) {
	file, err := ini.LoadFile(path)
	if err != nil {
		return nil, err
	}

	cp := &pgx.ConnectionParameters{}
	cp.Socket, _ = file.Get("database", "socket")
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

func (s *TernSuite) SelectValue(c *C, sql string, arguments ...interface{}) interface{} {
	value, err := s.conn.SelectValue(sql, arguments...)
	c.Assert(err, IsNil)
	return value
}

func (s *TernSuite) tableExists(c *C, tableName string) bool {
	return s.SelectValue(c,
		"select exists(select 1 from information_schema.tables where table_catalog=$1 and table_name=$2)",
		s.connParams.Database,
		tableName).(bool)
}

func (s *TernSuite) tern(c *C, args ...string) {
	cmd := exec.Command("tmp/tern", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		c.Fatalf("tern failed with: %v\noutput:\n%v", err, string(output))
	}
}

func (s *TernSuite) TestInitWithoutDirectory(c *C) {
	defer func() {
		os.Remove("tern.conf")
		os.Remove("001_create_people.sql.example")
	}()

	s.tern(c, "init")

	_, err := os.Stat("tern.conf")
	c.Assert(err, IsNil)

	_, err = os.Stat("001_create_people.sql.example")
	c.Assert(err, IsNil)
}

func (s *TernSuite) TestInitWithDirectory(c *C) {
	defer func() {
		os.RemoveAll("tmp/init")
	}()

	s.tern(c, "init", "tmp/init")

	_, err := os.Stat("tmp/init/tern.conf")
	c.Assert(err, IsNil)

	_, err = os.Stat("tmp/init/001_create_people.sql.example")
	c.Assert(err, IsNil)
}

func (s *TernSuite) TestNew(c *C) {
	path := "tmp/new"
	defer func() {
		os.RemoveAll(path)
	}()

	s.tern(c, "init", path)
	s.tern(c, "new", "-m", path, "first")

	_, err := os.Stat("tmp/new/001_first.sql")
	c.Assert(err, IsNil)

	s.tern(c, "new", "-m", path, "second")

	_, err = os.Stat("tmp/new/002_second.sql")
	c.Assert(err, IsNil)
}

func (s *TernSuite) TestMigrate(c *C) {
	// Ensure database is in clean state
	s.tern(c, "migrate", "-m", "testdata", "-c", "testdata/tern.conf", "-d", "0")
	c.Assert(s.tableExists(c, "t1"), Equals, false)
	c.Assert(s.tableExists(c, "t2"), Equals, false)

	// Up all the way
	s.tern(c, "migrate", "-m", "testdata", "-c", "testdata/tern.conf")
	c.Assert(s.tableExists(c, "t1"), Equals, true)
	c.Assert(s.tableExists(c, "t2"), Equals, true)

	// Back one
	s.tern(c, "migrate", "-m", "testdata", "-c", "testdata/tern.conf", "-d", "1")
	c.Assert(s.tableExists(c, "t1"), Equals, true)
	c.Assert(s.tableExists(c, "t2"), Equals, false)
}
