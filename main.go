package main

import (
	"errors"
	"fmt"
	"github.com/JackC/cli"
	"github.com/JackC/pgx"
	"github.com/JackC/tern/migrate"
	"github.com/vaughan0/go-ini"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const VERSION = "1.2.2"

var defaultConf = `[database]
# socket or host is required
# socket =
# host =
# port = 5432
# database is required
# database =
# user defaults to OS user
# user =
# version_table = schema_version

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
	ConnectionParameters pgx.ConnectionParameters
	VersionTable         string
	Data                 map[string]interface{}
}

func (c *Config) Validate() error {
	if c.ConnectionParameters.Host == "" && c.ConnectionParameters.Socket == "" {
		return errors.New("Config must contain host or socket but it does not")
	}

	if c.ConnectionParameters.Database == "" {
		return errors.New("Config must contain database but it does not")
	}

	return nil
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

	var conn *pgx.Connection
	conn, err = pgx.Connect(config.ConnectionParameters)
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
		fmt.Fprintf(os.Stderr, "Error migrating:\n  %v\n", err)
		os.Exit(1)
	}
}

func ReadConfig(path string) (*Config, error) {
	file, err := ini.LoadFile(path)
	if err != nil {
		return nil, err
	}

	config := &Config{VersionTable: "schema_version"}

	config.ConnectionParameters.Socket, _ = file.Get("database", "socket")
	config.ConnectionParameters.Host, _ = file.Get("database", "host")
	if p, ok := file.Get("database", "port"); ok {
		n, err := strconv.ParseUint(p, 10, 16)
		config.ConnectionParameters.Port = uint16(n)
		if err != nil {
			return nil, err
		}
	}

	config.ConnectionParameters.Database, _ = file.Get("database", "database")
	config.ConnectionParameters.User, _ = file.Get("database", "user")
	config.ConnectionParameters.Password, _ = file.Get("database", "password")

	if vt, ok := file.Get("database", "version_table"); ok {
		config.VersionTable = vt
	}

	config.Data = make(map[string]interface{})
	for key, value := range file["data"] {
		config.Data[key] = value
	}

	return config, nil
}
