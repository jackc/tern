package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/jackc/cli"
	"github.com/jackc/pgx"
	"github.com/jackc/tern/migrate"
	"github.com/vaughan0/go-ini"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const VERSION = "1.4.0"

var defaultConf = `[database]
# host is required (network host or path to Unix domain socket)
# host =
# port = 5432
# database is required
# database =
# user defaults to OS user
# user =
# version_table = schema_version
#
# sslmode generally matches the behavior described in:
# http://www.postgresql.org/docs/9.4/static/libpq-ssl.html#LIBPQ-SSL-PROTECTION
#
# There are only two modes that most users should use:
# prefer - on trusted networks where security is not required
# verify-full - require SSL connection
# sslmode = prefer

[data]
# Any fields in the data section are available in migration templates
# prefix = foo
`

var sampleMigration = `-- This is a sample migration.

create table people(
  id serial primary key,
  first_name varchar not null,
  last_name varchar not null
);

---- create above / drop below ----

drop table people;
`

var newMigrationText = `-- Write your migrate up statements here

---- create above / drop below ----

-- Write your migrate down statements here. If this migration is irreversible
-- Then delete the separator line above.
`

type Config struct {
	ConnConfig   pgx.ConnConfig
	SslMode      string
	VersionTable string
	Data         map[string]interface{}
}

func (c *Config) Validate() error {
	if c.ConnConfig.Host == "" {
		return errors.New("Config must contain host but it does not")
	}

	if c.ConnConfig.Database == "" {
		return errors.New("Config must contain database but it does not")
	}

	switch c.SslMode {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
		// okay
	default:
		return errors.New("sslmode is invalid")
	}

	return nil
}

func (c *Config) Connect() (*pgx.Conn, error) {
	switch c.SslMode {
	case "disable":
	case "allow":
		c.ConnConfig.UseFallbackTLS = true
		c.ConnConfig.FallbackTLSConfig = &tls.Config{InsecureSkipVerify: true}
	case "prefer":
		c.ConnConfig.TLSConfig = &tls.Config{InsecureSkipVerify: true}
		c.ConnConfig.UseFallbackTLS = true
		c.ConnConfig.FallbackTLSConfig = nil
	case "require", "verify-ca", "verify-full":
		c.ConnConfig.TLSConfig = &tls.Config{
			ServerName: c.ConnConfig.Host,
		}
	}

	return pgx.Connect(c.ConnConfig)
}

func main() {
	app := cli.NewApp()
	app.Name = "tern"
	app.Usage = "PostgreSQL database migrator"
	app.Version = VERSION
	app.Author = "Jack Christensen"
	app.Email = "jack@jackchristensen.com"

	app.Commands = []cli.Command{
		{
			Name:        "init",
			ShortName:   "i",
			Usage:       "init a new tern project",
			Synopsis:    "[directory]",
			Description: "Initialize a new tern project in directory",
			Action:      Init,
		},
		{
			Name:        "migrate",
			ShortName:   "m",
			Usage:       "migrate the database",
			Synopsis:    "[command options]",
			Description: "migrate the database to destination version",
			Flags: []cli.Flag{
				cli.StringFlag{"destination, d", "last", "Destination migration version"},
				cli.StringFlag{"migrations, m", ".", "Migrations path"},
				cli.StringFlag{"config, c", "tern.conf", "Config path"},
			},
			Action: Migrate,
		},
		{
			Name:        "new",
			ShortName:   "n",
			Usage:       "generate a new migration",
			Synopsis:    "[command options] name",
			Description: "generate a new migration with the next sequence number and provided name",
			Flags: []cli.Flag{
				cli.StringFlag{"migrations, m", ".", "Migrations path"},
			},
			Action: NewMigration,
		},
	}

	app.Run(os.Args)
}

func Init(c *cli.Context) {
	var directory string
	switch len(c.Args()) {
	case 0:
		directory = "."
	case 1:
		directory = c.Args()[0]
		err := os.Mkdir(directory, os.ModePerm)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		cli.ShowCommandHelp(c, c.Command.Name)
		os.Exit(1)
	}

	// Write default conf file
	confPath := filepath.Join(directory, "tern.conf")
	confFile, err := os.OpenFile(confPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.ModePerm)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer confFile.Close()

	_, err = confFile.WriteString(defaultConf)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Write sample migration
	smPath := filepath.Join(directory, "001_create_people.sql.example")
	smFile, err := os.OpenFile(smPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.ModePerm)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer smFile.Close()

	_, err = smFile.WriteString(sampleMigration)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func NewMigration(c *cli.Context) {
	if len(c.Args()) != 1 {
		cli.ShowCommandHelp(c, c.Command.Name)
		os.Exit(1)
	}

	name := c.Args()[0]

	migrationsPath := c.String("migrations")
	migrations, err := migrate.FindMigrations(migrationsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading migrations:\n  %v\n", err)
		os.Exit(1)
	}

	newMigrationName := fmt.Sprintf("%03d_%s.sql", len(migrations)+1, name)

	// Write new migration
	mPath := filepath.Join(migrationsPath, newMigrationName)
	mFile, err := os.OpenFile(mPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.ModePerm)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer mFile.Close()

	_, err = mFile.WriteString(newMigrationText)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

}

func Migrate(c *cli.Context) {
	config, err := ReadConfig(c.String("config"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading config:\n  %v\n", err)
		os.Exit(1)
	}

	err = config.Validate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid config:\n  %v\n", err)
		os.Exit(1)
	}

	conn, err := config.Connect()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to connect to PostgreSQL:\n  %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	migrator, err := migrate.NewMigrator(conn, config.VersionTable)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing migrator:\n  %v\n", err)
		os.Exit(1)
	}
	migrator.Data = config.Data

	migrationsPath := c.String("migrations")
	err = migrator.LoadMigrations(migrationsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading migrations:\n  %v\n", err)
		os.Exit(1)
	}
	if len(migrator.Migrations) == 0 {
		fmt.Fprintln(os.Stderr, "No migrations found")
		os.Exit(1)
	}

	migrator.OnStart = func(sequence int32, name, direction, sql string) {
		fmt.Printf("%s executing %s %s\n%s\n\n", time.Now().Format("2006-01-02 15:04:05"), name, direction, sql)
	}

	destination := c.String("destination")
	if destination == "last" {
		err = migrator.Migrate()
	} else {
		var n int64
		n, err = strconv.ParseInt(destination, 10, 32)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Bad destination:\n  %v\n", err)
			os.Exit(1)
		}

		err = migrator.MigrateTo(int32(n))
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)

		if err, ok := err.(migrate.MigrationSyntaxError); ok {
			ele, err := ExtractErrorLine(err.Sql, int(err.Position))
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			prefix := fmt.Sprintf("LINE %d: ", ele.LineNum)
			fmt.Printf("%s%s\n", prefix, ele.Text)

			padding := strings.Repeat(" ", len(prefix)+ele.ColumnNum-1)
			fmt.Printf("%s^\n", padding)
		}
		os.Exit(1)
	}
}

func ReadConfig(path string) (*Config, error) {
	file, err := ini.LoadFile(path)
	if err != nil {
		return nil, err
	}

	config := &Config{VersionTable: "schema_version"}

	config.ConnConfig.Host, _ = file.Get("database", "host")

	// For backwards compatibility if host isn't set look for socket.
	if config.ConnConfig.Host == "" {
		config.ConnConfig.Host, _ = file.Get("database", "socket")
	}

	if p, ok := file.Get("database", "port"); ok {
		n, err := strconv.ParseUint(p, 10, 16)
		config.ConnConfig.Port = uint16(n)
		if err != nil {
			return nil, err
		}
	}

	config.ConnConfig.Database, _ = file.Get("database", "database")
	config.ConnConfig.User, _ = file.Get("database", "user")
	config.ConnConfig.Password, _ = file.Get("database", "password")

	if vt, ok := file.Get("database", "version_table"); ok {
		config.VersionTable = vt
	}

	if sslmode, ok := file.Get("database", "sslmode"); ok {
		config.SslMode = sslmode
	} else {
		config.SslMode = "prefer"
	}

	config.Data = make(map[string]interface{})
	for key, value := range file["data"] {
		config.Data[key] = value
	}

	return config, nil
}
