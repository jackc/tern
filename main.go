package main

import (
	"errors"
	"fmt"
	"github.com/JackC/pgx"
	"github.com/JackC/tern/migrate"
	"github.com/codegangsta/cli"
	"github.com/vaughan0/go-ini"
	"os"
	"strconv"
	"time"
)

const VERSION = "1.1.1"

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
	app.Flags = []cli.Flag{
		cli.StringFlag{"destination, d", "last", "Destination migration version"},
		cli.StringFlag{"migrations, m", ".", "Migrations path"},
		cli.StringFlag{"config, c", "tern.conf", "Config path"},
	}
	app.Action = Migrate
	app.Run(os.Args)
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
