package main

import (
	"errors"
	"fmt"
	"github.com/JackC/pgx"
	"github.com/JackC/tern/migrate"
	"github.com/jessevdk/go-flags"
	"github.com/vaughan0/go-ini"
	"os"
	"strconv"
	"time"
)

const VERSION = "1.1.0"

type Opts struct {
	Destination    string `short:"d" long:"destination" description:"Destination migration version" default:"last"`
	MigrationsPath string `short:"m" long:"migrations" description:"Migrations path" default:"."`
	ConfigPath     string `short:"c" long:"config" description:"Config path" default:"tern.conf"`
	Version        bool   `long:"version" description:"Display version and exit"`
}

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
	var err error
	var opts Opts

	parser := flags.NewParser(&opts, flags.Default)
	_, err = parser.Parse()
	if err != nil {
		os.Exit(1)
	}

	if opts.Version {
		fmt.Println("tern " + VERSION)
		os.Exit(0)
	}

	config, err := ReadConfig(opts.ConfigPath)
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

	err = migrator.LoadMigrations(opts.MigrationsPath)
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

	if opts.Destination == "last" {
		err = migrator.Migrate()
	} else {
		var n int64
		n, err = strconv.ParseInt(opts.Destination, 10, 32)
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
