package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/JackC/pgx"
	"github.com/JackC/tern/migrate"
	"github.com/jessevdk/go-flags"
	"io/ioutil"
	"os"
	"strconv"
	"time"
)

const VERSION = "1.0.0"

type Opts struct {
	Destination    string `short:"d" long:"destination" description:"Destination migration version" default:"last"`
	MigrationsPath string `short:"m" long:"migrations" description:"Migrations path" default:"."`
	ConfigPath     string `short:"c" long:"config" description:"Config path" default:"tern.json"`
	Version        bool   `long:"version" description:"Display version and exit"`
}

type Config struct {
	Socket       string `json:socket`
	Host         string `json:host`
	Port         uint16 `json:port`
	Database     string `json:database`
	User         string `json:user`
	Password     string `json:password`
	VersionTable string `json:versionTable`
}

func (c *Config) ConnectionParameters() pgx.ConnectionParameters {
	var p pgx.ConnectionParameters
	p.Socket = c.Socket
	p.Host = c.Host
	p.Port = c.Port
	p.Database = c.Database
	p.User = c.User
	p.Password = c.Password
	return p
}

func (c *Config) Validate() error {
	if c.Host == "" && c.Socket == "" {
		return errors.New("Config must contain host or socket but it does not")
	}

	if c.Database == "" {
		return errors.New("Config must contain database but it does not")
	}

	if c.User == "" {
		return errors.New("Config must contain user but it does not")
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

	pgx.Connect(config.ConnectionParameters())

	var conn *pgx.Connection
	conn, err = pgx.Connect(config.ConnectionParameters())
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

	err = migrator.LoadMigrations(opts.MigrationsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading migrations:\n  %v\n", err)
		os.Exit(1)
	}
	if len(migrator.Migrations) == 0 {
		fmt.Fprintln(os.Stderr, "No migrations found")
		os.Exit(1)
	}

	migrator.OnStart = func(migration *migrate.Migration, direction string) {
		var sql string
		if direction == "up" {
			sql = migration.UpSQL
		} else {
			sql = migration.DownSQL
		}

		fmt.Printf("%s executing %s %s\n%s\n\n", time.Now().Format("2006-01-02 15:04:05"), migration.Name, direction, sql)
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
	configBytes, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	config := &Config{VersionTable: "schema_version"}
	err = json.Unmarshal(configBytes, config)
	if err != nil {
		return nil, err
	}

	return config, nil
}
