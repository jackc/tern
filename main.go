package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/tern/v2/migrate"
	"github.com/spf13/cobra"
	ini "github.com/vaughan0/go-ini"
)

const VERSION = "2.3.2"

var defaultConf = `[database]
# host is required (network host or path to Unix domain socket)
# host =
# port = 5432
# database is required
# database =
# user defaults to OS user
# user =
# password =
# version_table = public.schema_version
#
# sslmode generally matches the behavior described in:
# http://www.postgresql.org/docs/9.4/static/libpq-ssl.html#LIBPQ-SSL-PROTECTION
#
# There are only two modes that most users should use:
# prefer - on trusted networks where security is not required
# verify-full - require SSL connection
# sslmode = prefer
#
# sslrootcert is generally used with sslmode=verify-full
# sslrootcert = /path/to/root/ca

# Proxy the above database connection via SSH
# [ssh-tunnel]
# host =
# port = 22
# user defaults to OS user
# user =
# password is not required if using SSH agent authentication
# password =

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
	ConnConfig    pgx.ConnConfig
	ConnString    string
	PGEnvvars     map[string]string
	VersionTable  string
	Data          map[string]interface{}
	SSHConnConfig SSHConnConfig
}

var cliOptions struct {
	destinationVersion string
	currentVersion     string
	migrationsPath     string
	configPaths        []string
	editNewMigration   bool
	outputFile         string // used for gengen or print-migrations

	connString   string
	host         string
	port         uint16
	user         string
	password     string
	database     string
	sslmode      string
	sslrootcert  string
	versionTable string

	sshHost       string
	sshPort       string
	sshKeyFile    string
	sshPassphrase string
	sshUser       string
	sshPassword   string
}

func (c *Config) Validate() error {
	if c.ConnConfig.Host == "" {
		return errors.New("Config must contain host but it does not")
	}

	if c.ConnConfig.Database == "" {
		return errors.New("Config must contain database but it does not")
	}

	return nil
}

func (c *Config) Connect(ctx context.Context) (*pgx.Conn, error) {
	if c.SSHConnConfig.Host != "" {
		client, err := NewSSHClient(&c.SSHConnConfig)
		if err != nil {
			return nil, err
		}

		c.ConnConfig.DialFunc = func(ctx context.Context, network string, addr string) (net.Conn, error) {
			return client.Dial(network, addr)
		}
	}

	return pgx.ConnectConfig(ctx, &c.ConnConfig)
}

func main() {
	cmdInit := &cobra.Command{
		Use:   "init DIRECTORY",
		Short: "Initialize a new tern project",
		Long:  "Initialize a new tern project in DIRECTORY",
		Run:   Init,
	}

	cmdMigrate := &cobra.Command{
		Use:   "migrate",
		Short: "Migrate the database",
		Long: `Migrate the database to destination migration version.

Destination migration version can be one of the following value types:

An integer:
  Migrate to a specific migration.
  e.g. tern migrate -d 42

"+" and an integer:
  Migrate forward N steps.
  e.g. tern migrate -d +3

"-" and an integer:
  Migrate backward N steps.
  e.g. tern migrate -d -2

"-+" and an integer:
  Redo previous N steps (migrate backward N steps then forward N steps).
  e.g. tern migrate -d -+1

The word "last":
  Migrate to the most recent migration. This is the default value, so it is
  never needed to specify directly.
  e.g. tern migrate
  e.g. tern migrate -d last
		`,
		Run: Migrate,
	}
	cmdMigrate.Flags().StringVarP(&cliOptions.destinationVersion, "destination", "d", "last", "destination migration version")
	addConfigFlagsToCommand(cmdMigrate)

	cmdCode := &cobra.Command{
		Use:   "code COMMAND",
		Short: "Execute a code package command",
	}

	cmdCodeInstall := &cobra.Command{
		Use:   "install PATH",
		Short: "Install a code package into the database",
		Args:  cobra.ExactArgs(1),
		Run:   InstallCode,
	}
	addCoreConfigFlagsToCommand(cmdCodeInstall)

	cmdCodeCompile := &cobra.Command{
		Use:   "compile PATH",
		Short: "Compile a code package into SQL",
		Args:  cobra.ExactArgs(1),
		Run:   CompileCode,
	}
	cmdCodeCompile.Flags().StringSliceVarP(&cliOptions.configPaths, "config", "c", []string{}, "config path (default is ./tern.conf)")

	cmdCodeSnapshot := &cobra.Command{
		Use:   "snapshot PATH",
		Short: "Snapshot a code package into a migration",
		Args:  cobra.ExactArgs(1),
		Run:   SnapshotCode,
	}
	cmdCodeSnapshot.Flags().StringVarP(&cliOptions.migrationsPath, "migrations", "m", "", "migrations path (default is .)")

	cmdStatus := &cobra.Command{
		Use:   "status",
		Short: "Print current migration status",
		Run:   Status,
	}
	addConfigFlagsToCommand(cmdStatus)

	cmdPrintConnString := &cobra.Command{
		Use:   "print-connstring",
		Short: "Prints a connection string based on the provided config file/arguments",
		Run:   PrintConnString,
	}
	addConfigFlagsToCommand(cmdPrintConnString)

	cmdNew := &cobra.Command{
		Use:   "new NAME",
		Short: "Generate a new migration",
		Long:  "Generate a new migration with the next sequence number and provided name",
		Run:   NewMigration,
	}
	cmdNew.Flags().StringVarP(&cliOptions.migrationsPath, "migrations", "m", "", "migrations path (default is .)")
	cmdNew.Flags().BoolVarP(&cliOptions.editNewMigration, "edit", "e", false, "open new migration in EDITOR")

	cmdRenumber := &cobra.Command{
		Use:   "renumber COMMAND",
		Short: "Execute a renumber command",
	}

	cmdRenumberStart := &cobra.Command{
		Use:   "start",
		Short: "Start renumbering",
		Long:  "Start renumbering with the current migration numbering preserved",
		Run:   RenumberStart,
	}
	cmdRenumberStart.Flags().StringVarP(&cliOptions.migrationsPath, "migrations", "m", "", "migrations path (default is .)")

	cmdRenumberFinish := &cobra.Command{
		Use:   "finish",
		Short: "Finish renumbering",
		Long:  "Finish renumbering with new migrations renumbered",

		Run: RenumberFinish,
	}
	cmdRenumberFinish.Flags().StringVarP(&cliOptions.migrationsPath, "migrations", "m", "", "migrations path (default is .)")

	cmdGengen := &cobra.Command{
		Use:   "gengen",
		Short: "Generate a SQL script that generates migrations",
		Long: `Generate a SQL script that generates migrations.

This generates a SQL script that when run against a database will generate a
SQL script with the migrations necessary to bring the database to the latest
schema version.

This can be useful in situations where tern cannot be used directly. For
example, a plugin or application using a shared database may need database
migrations, but another system is the only system that can perform database
migrations. In this case, the script generated by gengen can be used to
generate a migration script that can be run by the other system.

Limitations:

Every migration runs in a transaction. That is, if a migration has a
disable-tx magic comment it will be ignored.

Migrations can only go forward to the latest version.
`,
		Run: Gengen,
	}
	cmdGengen.Flags().StringSliceVarP(&cliOptions.configPaths, "config", "c", []string{}, "config path (default is ./tern.conf)")
	cmdGengen.Flags().StringVarP(&cliOptions.versionTable, "version-table", "", "", "version table name (default is public.schema_version)")
	cmdGengen.Flags().StringVarP(&cliOptions.migrationsPath, "migrations", "m", "", "migrations path (default is .)")
	cmdGengen.Flags().StringVarP(&cliOptions.outputFile, "output", "o", "", "output file")

	cmdPrintMigrations := &cobra.Command{
		Use:   "print-migrations",
		Short: "Print the migrations",
		Long: `Print migrations

This prints the migrations into a single file or to the console.

This can be useful if the SQL in the migrations is needed by other tools,
such as programs for code generation (e.g., sqlc.dev) or tools for analyzing
the DDL.

Note that the generated SQL file is not intended to be executed directly
against your database, as it does not update the version table nor does
it do any error handling

`,
		Run: PrintMigrations,
	}
	addCoreConfigFlagsToCommand(cmdPrintMigrations)
	cmdPrintMigrations.Flags().StringVarP(&cliOptions.currentVersion, "current", "", "0", "current version of the database (use from_db to read the current version form the database)")
	cmdPrintMigrations.Flags().StringVarP(&cliOptions.destinationVersion, "destination", "d", "last", "destination migration version")
	cmdPrintMigrations.Flags().StringVarP(&cliOptions.migrationsPath, "migrations", "m", "", "migrations path (default is .)")
	cmdPrintMigrations.Flags().StringVarP(&cliOptions.outputFile, "output", "o", "", "output file")

	cmdVersion := &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("tern v%s\n", VERSION)
		},
	}

	cmdCode.AddCommand(cmdCodeInstall)
	cmdCode.AddCommand(cmdCodeCompile)
	cmdCode.AddCommand(cmdCodeSnapshot)

	cmdRenumber.AddCommand(cmdRenumberStart)
	cmdRenumber.AddCommand(cmdRenumberFinish)

	rootCmd := &cobra.Command{Use: "tern", Short: "tern - PostgreSQL database migrator"}
	rootCmd.AddCommand(cmdInit)
	rootCmd.AddCommand(cmdMigrate)
	rootCmd.AddCommand(cmdRenumber)
	rootCmd.AddCommand(cmdCode)
	rootCmd.AddCommand(cmdStatus)
	rootCmd.AddCommand(cmdPrintConnString)
	rootCmd.AddCommand(cmdNew)
	rootCmd.AddCommand(cmdGengen)
	rootCmd.AddCommand(cmdPrintMigrations)
	rootCmd.AddCommand(cmdVersion)
	rootCmd.Execute()
}

func addCoreConfigFlagsToCommand(cmd *cobra.Command) {
	cmd.Flags().StringSliceVarP(&cliOptions.configPaths, "config", "c", []string{}, "config path (default is ./tern.conf)")

	cmd.Flags().StringVarP(&cliOptions.connString, "conn-string", "", "", "database connection string (https://www.postgresql.org/docs/current/libpq-connect.html#LIBPQ-CONNSTRING)")
	cmd.Flags().StringVarP(&cliOptions.host, "host", "", "", "database host")
	cmd.Flags().Uint16VarP(&cliOptions.port, "port", "", 0, "database port")
	cmd.Flags().StringVarP(&cliOptions.user, "user", "", "", "database user")
	cmd.Flags().StringVarP(&cliOptions.password, "password", "", "", "database password")
	cmd.Flags().StringVarP(&cliOptions.database, "database", "", "", "database name")
	cmd.Flags().StringVarP(&cliOptions.sslmode, "sslmode", "", "", "SSL mode")
	cmd.Flags().StringVarP(&cliOptions.sslrootcert, "sslrootcert", "", "", "SSL root certificate")
	cmd.Flags().StringVarP(&cliOptions.versionTable, "version-table", "", "", "version table name (default is public.schema_version)")

	cmd.Flags().StringVarP(&cliOptions.sshHost, "ssh-host", "", "", "SSH tunnel host")
	cmd.Flags().StringVarP(&cliOptions.sshPort, "ssh-port", "", "", "SSH tunnel port")
	cmd.Flags().StringVarP(&cliOptions.sshKeyFile, "ssh-keyfile", "", "", "Path to SSH key file to use")
	cmd.Flags().StringVarP(&cliOptions.sshPassphrase, "ssh-passphrase", "", "", "Passphrase for SSH key file (only required if file is encrypted)")
	cmd.Flags().StringVarP(&cliOptions.sshUser, "ssh-user", "", "", "SSH tunnel user (default is OS user")
	cmd.Flags().StringVarP(&cliOptions.sshPassword, "ssh-password", "", "", "SSH tunnel password (unneeded if using SSH agent authentication)")
}

func addConfigFlagsToCommand(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&cliOptions.migrationsPath, "migrations", "m", "", "migrations path (default is .)")
	addCoreConfigFlagsToCommand(cmd)
}

func Init(cmd *cobra.Command, args []string) {
	var directory string
	switch len(args) {
	case 0:
		directory = "."
	case 1:
		directory = args[0]
		err := os.Mkdir(directory, os.ModePerm)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		cmd.Help()
		os.Exit(1)
	}

	// Write default conf file
	confPath := filepath.Join(directory, "tern.conf")
	confFile, err := os.OpenFile(confPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o666)
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
	smFile, err := os.OpenFile(smPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o666)
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

func NewMigration(cmd *cobra.Command, args []string) {
	if len(args) != 1 {
		cmd.Help()
		os.Exit(1)
	}

	name := args[0]

	// If no migrations path was set in CLI argument look in environment.
	if cliOptions.migrationsPath == "" {
		cliOptions.migrationsPath = os.Getenv("TERN_MIGRATIONS")
	}

	// If no migrations path was set in CLI argument or environment use default.
	if cliOptions.migrationsPath == "" {
		cliOptions.migrationsPath = "."
	}

	migrationsPath := cliOptions.migrationsPath
	migrations, err := migrate.FindMigrations(os.DirFS(migrationsPath))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading migrations:\n  %v\n", err)
		os.Exit(1)
	}

	newMigrationName := fmt.Sprintf("%03d_%s.sql", len(migrations)+1, name)

	// Write new migration
	mPath := filepath.Join(migrationsPath, newMigrationName)
	mFile, err := os.OpenFile(mPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o666)
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

	if cliOptions.editNewMigration {
		editor := os.Getenv("EDITOR")
		if editor == "" {
			fmt.Fprintln(os.Stderr, "EDITOR environment variable not set")
			os.Exit(1)
		}

		cmd := exec.Command("sh", "-c", fmt.Sprintf("%s '%s'", editor, mPath))
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Failed to start editor:", err)
			os.Exit(1)
		}
	}
}

func loadConfigAndConnectToDB(ctx context.Context) (*Config, *pgx.Conn) {
	config, err := LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config:\n  %v\n", err)
		os.Exit(1)
	}

	err = config.Validate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid config:\n  %v\n", err)
		os.Exit(1)
	}

	conn, err := config.Connect(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to connect to PostgreSQL:\n  %v\n", err)
		os.Exit(1)
	}

	return config, conn
}

func Migrate(cmd *cobra.Command, args []string) {
	ctx := context.Background()
	config, conn := loadConfigAndConnectToDB(ctx)
	defer conn.Close(ctx)

	migrator, err := migrate.NewMigrator(ctx, conn, config.VersionTable)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing migrator:\n  %v\n", err)
		os.Exit(1)
	}
	migrator.Data = config.Data

	migrationsPath := cliOptions.migrationsPath
	err = migrator.LoadMigrations(os.DirFS(migrationsPath))
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

	var currentVersion int32
	currentVersion, err = migrator.GetCurrentVersion(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to get current version:\n  %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	interruptChan := make(chan os.Signal, 1)
	signal.Notify(interruptChan, os.Interrupt)
	go func() {
		<-interruptChan
		cancel()       // Cancel any in progress migrations
		signal.Reset() // Only listen for one interrupt. If another interrupt signal is received allow it to terminate the program.
	}()

	destination := cliOptions.destinationVersion
	mustParseDestination := func(d string) int32 {
		var n int64
		n, err = strconv.ParseInt(d, 10, 32)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Bad destination:\n  %v\n", err)
			os.Exit(1)
		}
		return int32(n)
	}
	if destination == "last" {
		err = migrator.Migrate(ctx)
	} else if len(destination) >= 3 && destination[0:2] == "-+" {
		err = migrator.MigrateTo(ctx, currentVersion-mustParseDestination(destination[2:]))
		if err == nil {
			err = migrator.MigrateTo(ctx, currentVersion)
		}
	} else if len(destination) >= 2 && destination[0] == '-' {
		err = migrator.MigrateTo(ctx, currentVersion-mustParseDestination(destination[1:]))
	} else if len(destination) >= 2 && destination[0] == '+' {
		err = migrator.MigrateTo(ctx, currentVersion+mustParseDestination(destination[1:]))
	} else {
		err = migrator.MigrateTo(ctx, mustParseDestination(destination))
	}

	if err != nil {
		if mgErr, ok := err.(migrate.MigrationPgError); ok {
			fmt.Fprintln(os.Stderr, mgErr.PgError)

			if mgErr.Detail != "" {
				fmt.Fprintln(os.Stderr, "DETAIL:", mgErr.Detail)
			}

			if mgErr.Position != 0 {
				ele, err := migrate.ExtractErrorLine(mgErr.Sql, int(mgErr.Position))
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}

				prefix := fmt.Sprintf("LINE %d: ", ele.LineNum)
				fmt.Fprintf(os.Stderr, "%s%s\n", prefix, ele.Text)

				padding := strings.Repeat(" ", len(prefix)+ele.ColumnNum-1)
				fmt.Fprintf(os.Stderr, "%s^\n", padding)
			}
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}

func Gengen(cmd *cobra.Command, args []string) {
	config, err := LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config:\n  %v\n", err)
		os.Exit(1)
	}

	err = config.Validate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid config:\n  %v\n", err)
		os.Exit(1)
	}

	migrator, err := migrate.NewMigrator(context.Background(), nil, config.VersionTable)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing migrator:\n  %v\n", err)
		os.Exit(1)
	}
	migrator.Data = config.Data

	migrationsPath := cliOptions.migrationsPath
	err = migrator.LoadMigrations(os.DirFS(migrationsPath))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading migrations:\n  %v\n", err)
		os.Exit(1)
	}
	if len(migrator.Migrations) == 0 {
		fmt.Fprintln(os.Stderr, "No migrations found")
		os.Exit(1)
	}

	gengenTemplate := template.Must(template.New("gengen").Parse(`-- This file was generated by tern gengen v{{ .Version }}.
--
-- If using psql to execute this script use the --no-psqlrc, --tuples-only,
-- --quiet, and --no-align options to only output the migration SQL.
--
-- e.g. psql --no-psqlrc --tuples-only --quiet --no-align -f this_file.sql
--
-- The results can be redirected to a file where the proposed changes can be
-- inspected or the results can be piped back into psql to migrate immediately.
--
-- e.g. psql --no-psqlrc --tuples-only --quiet --no-align -f this_file.sql | psql

set tern.version = -1;
do $$
declare
	schema_version_table_exists boolean;
begin
	select to_regclass('{{ .VersionTable }}') is not null into schema_version_table_exists;
	if schema_version_table_exists then
		perform set_config('tern.version', version::text, false) from {{ .VersionTable }};
	end if;
end
$$;

with migrations(version, up_sql) as (
	values
	(0,
$tern_gengen$
begin;
create table {{ .VersionTable }}(version int4 not null);
insert into {{ .VersionTable }}(version) values(0);
$tern_gengen$)
{{ range .Migrations }}
, ({{ .Sequence }},
$tern_gengen$
-- {{ .Name }}
begin;
{{ .UpSQL }}$tern_gengen$)
{{ end }}
)
select up_sql || '
update {{ .VersionTable }} set version = ' || version || ';
commit;
'
from migrations
where version > current_setting('tern.version')::int4
order by version asc;

`))

	var out *os.File
	if cliOptions.outputFile == "" {
		out = os.Stdout
	} else {
		var err error
		out, err = os.Create(cliOptions.outputFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer out.Close()
	}

	err = gengenTemplate.Execute(out, map[string]any{
		"Version":      VERSION,
		"VersionTable": config.VersionTable,
		"Migrations":   migrator.Migrations,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error generating gengen script:", err)
		os.Exit(1)
	}
}

func InstallCode(cmd *cobra.Command, args []string) {
	path := args[0]

	codePackage, err := migrate.LoadCodePackage(os.DirFS(path))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load code package:\n  %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	config, conn := loadConfigAndConnectToDB(ctx)
	defer conn.Close(ctx)

	sql, err := codePackage.Eval(config.Data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to evaluate code package:\n  %v\n", err)
		os.Exit(1)
	}

	err = migrate.LockExecTx(ctx, conn, sql)
	if err != nil {
		if migrationpgError, ok := err.(migrate.MigrationPgError); ok {
			fmt.Fprintln(os.Stderr, migrationpgError)
			if migrationpgError.Detail != "" {
				fmt.Fprintln(os.Stderr, "DETAIL:", migrationpgError.Detail)
			}

			if migrationpgError.Position != 0 {
				ele, err := migrate.ExtractErrorLine(migrationpgError.Sql, int(migrationpgError.Position))
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}

				prefix := fmt.Sprintf("LINE %d: ", ele.LineNum)
				fmt.Fprintf(os.Stderr, "%s%s\n", prefix, ele.Text)

				padding := strings.Repeat(" ", len(prefix)+ele.ColumnNum-1)
				fmt.Fprintf(os.Stderr, "%s^\n", padding)
			}
		} else {
			fmt.Fprintf(os.Stderr, "Failed to install code package:\n  %v\n", err)
		}
		os.Exit(1)
	}
}

func CompileCode(cmd *cobra.Command, args []string) {
	path := args[0]

	codePackage, err := migrate.LoadCodePackage(os.DirFS(path))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load code package:\n  %v\n", err)
		os.Exit(1)
	}

	config, err := LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config:\n  %v\n", err)
		os.Exit(1)
	}

	err = config.Validate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid config:\n  %v\n", err)
		os.Exit(1)
	}

	sql, err := codePackage.Eval(config.Data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to evaluate code package:\n  %v\n", err)
		os.Exit(1)
	}

	fmt.Println(sql)
}

func SnapshotCode(cmd *cobra.Command, args []string) {
	path := args[0]

	_, err := migrate.LoadCodePackage(os.DirFS(path))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load code package:\n  %v\n", err)
		os.Exit(1)
	}

	migrationsPath := cliOptions.migrationsPath

	// If no migrations path was set in CLI argument look in environment.
	if migrationsPath == "" {
		migrationsPath = os.Getenv("TERN_MIGRATIONS")
	}

	migrations, err := migrate.FindMigrations(os.DirFS(migrationsPath))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading migrations:\n  %v\n", err)
		os.Exit(1)
	}

	migrationID := fmt.Sprintf("%03d", len(migrations)+1)
	snapshotPath := filepath.Join(migrationsPath, "snapshots", migrationID)
	err = copyCodePackageDir(path, snapshotPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error copying snapshot:\n  %v\n", err)
		os.Exit(1)
	}

	newMigrationName := fmt.Sprintf("%s_install_%s.sql", migrationID, filepath.Base(path))

	// Write new migration
	mPath := filepath.Join(migrationsPath, newMigrationName)
	mFile, err := os.OpenFile(mPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o666)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer mFile.Close()

	_, err = fmt.Fprintf(mFile, `{{ install_snapshot "%s" }}`, migrationID)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func PrintConnString(cmd *cobra.Command, args []string) {
	config, err := LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	connstring := config.ConnString
	if connstring == "" {
		options := ""
		if ssl_mode := config.PGEnvvars["PGSSLMODE"]; ssl_mode != "" {
			options += fmt.Sprintf("sslmode=%s&", ssl_mode)
		}
		if ssl_cert := config.PGEnvvars["PGSSLROOTCERT"]; ssl_cert != "" {
			options += fmt.Sprintf("sslrootcert=%s", ssl_cert)
		}
		connstring = fmt.Sprintf(
			"postgres://%s:%s@%s:%d/%s?%s",
			config.ConnConfig.User,
			config.ConnConfig.Password,
			config.ConnConfig.Host,
			config.ConnConfig.Port,
			config.ConnConfig.Database,
			options,
		)
	}
	fmt.Print(connstring)
}

func Status(cmd *cobra.Command, args []string) {
	ctx := context.Background()
	config, conn := loadConfigAndConnectToDB(ctx)
	defer conn.Close(ctx)

	migrator, err := migrate.NewMigrator(ctx, conn, config.VersionTable)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing migrator:\n  %v\n", err)
		os.Exit(1)
	}
	migrator.Data = config.Data

	migrationsPath := cliOptions.migrationsPath
	err = migrator.LoadMigrations(os.DirFS(migrationsPath))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading migrations:\n  %v\n", err)
		os.Exit(1)
	}
	if len(migrator.Migrations) == 0 {
		fmt.Fprintln(os.Stderr, "No migrations found")
		os.Exit(1)
	}

	migrationVersion, err := migrator.GetCurrentVersion(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error retrieving migration version:\n  %v\n", err)
		os.Exit(1)
	}

	var status string
	behindCount := len(migrator.Migrations) - int(migrationVersion)
	if behindCount == 0 {
		status = "up to date"
	} else {
		status = "migration(s) pending"
	}

	fmt.Println("status:  ", status)
	fmt.Printf("version:  %d of %d\n", migrationVersion, len(migrator.Migrations))
	fmt.Println("host:    ", config.ConnConfig.Host)
	fmt.Println("database:", config.ConnConfig.Database)
}

func RenumberStart(cmd *cobra.Command, args []string) {
	migrationsPath := cliOptions.migrationsPath
	migrations, err := migrate.FindMigrations(os.DirFS(migrationsPath))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading migrations:\n  %v\n", err)
		os.Exit(1)
	}

	renumberFilepath := filepath.Join(migrationsPath, ".tern-renumber.tmp")
	renumberFile, err := os.Create(renumberFilepath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating renumber file:\n  %v\n", err)
		os.Exit(1)
	}
	defer renumberFile.Close()

	w := bufio.NewWriter(renumberFile)
	for _, s := range migrations {
		s = strings.TrimPrefix(s, migrationsPath)
		s = strings.TrimPrefix(s, string(filepath.Separator))
		_, err := fmt.Fprintln(w, s)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing renumber file:\n  %v\n", err)
			os.Exit(1)
		}
	}

	err = w.Flush()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing renumber file:\n  %v\n", err)
		os.Exit(1)
	}
}

func RenumberFinish(cmd *cobra.Command, args []string) {
	migrationsPath := cliOptions.migrationsPath

	currentMigrations, err := findMigrationsForRenumber(migrationsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading migrations:\n  %v\n", err)
		os.Exit(1)
	}

	for i, s := range currentMigrations {
		s = strings.TrimPrefix(s, migrationsPath)
		s = strings.TrimPrefix(s, string(filepath.Separator))
		currentMigrations[i] = s
	}

	renumberFilepath := filepath.Join(migrationsPath, ".tern-renumber.tmp")
	renumberFile, err := os.Open(renumberFilepath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening renumber file:\n  %v\n", err)
		os.Exit(1)
	}
	defer renumberFile.Close()

	var originalMigrations []string
	scanner := bufio.NewScanner(renumberFile)
	for scanner.Scan() {
		originalMigrations = append(originalMigrations, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Error reading renumber file:\n  %v\n", err)
		os.Exit(1)
	}

	numberPrefixRegexp := regexp.MustCompile(`^\d+`)
	var lastMigrationNumber int64
	for _, s := range originalMigrations {
		numStr := numberPrefixRegexp.FindString(s)
		num, err := strconv.ParseInt(numStr, 10, 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing renumber file:\n  %v\n", err)
			os.Exit(1)
		}

		if num > lastMigrationNumber {
			lastMigrationNumber = num
		}
	}

	makeMapFromStrings := func(slice []string) map[string]struct{} {
		m := make(map[string]struct{})
		for _, s := range slice {
			m[s] = struct{}{}
		}
		return m
	}
	originalMigrationsMap := makeMapFromStrings(originalMigrations)

	var migrationsToRenumber []string
	for _, s := range currentMigrations {
		if _, present := originalMigrationsMap[s]; !present {
			migrationsToRenumber = append(migrationsToRenumber, s)
		}
	}

	sort.Slice(migrationsToRenumber, func(i, j int) bool {
		iStr := numberPrefixRegexp.FindString(migrationsToRenumber[i])
		iNum, _ := strconv.ParseInt(iStr, 10, 64)
		jStr := numberPrefixRegexp.FindString(migrationsToRenumber[j])
		jNum, _ := strconv.ParseInt(jStr, 10, 64)
		return iNum < jNum
	})

	for _, s := range migrationsToRenumber {
		numPrefix := numberPrefixRegexp.FindString(s)
		lastMigrationNumber++
		newMigrationName := fmt.Sprintf("%03d%s", lastMigrationNumber, s[len(numPrefix):])
		err := os.Rename(filepath.Join(migrationsPath, s), filepath.Join(migrationsPath, newMigrationName))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error renaming migration file:\n  %v\n", err)
			os.Exit(1)
		}
	}

	os.Remove(renumberFilepath)
}

// findMigrationsForRenumber finds migration files. Can't use migrate.FindMigrations because it fails when there are
// duplicate numbers.
func findMigrationsForRenumber(path string) ([]string, error) {
	migrationPattern := regexp.MustCompile(`\A(\d+)_.+\.sql\z`)

	path = strings.TrimRight(path, string(filepath.Separator))

	fileInfos, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	paths := make([]string, 0, len(fileInfos))
	for _, fi := range fileInfos {
		if fi.IsDir() {
			continue
		}

		matches := migrationPattern.FindStringSubmatch(fi.Name())
		if len(matches) != 2 {
			continue
		}

		paths = append(paths, filepath.Join(path, fi.Name()))
	}

	return paths, nil
}

func LoadConfig() (*Config, error) {
	config := &Config{
		PGEnvvars:    make(map[string]string),
		VersionTable: "public.schema_version",
		Data:         make(map[string]interface{}),
	}
	// If no config path was set in CLI argument look in environment.
	if len(cliOptions.configPaths) == 0 {
		if configEnv := os.Getenv("TERN_CONFIG"); configEnv != "" {
			cliOptions.configPaths = append(cliOptions.configPaths, configEnv)
		}
	}

	// If no config path was set in CLI or environment try default location.
	if len(cliOptions.configPaths) == 0 {
		if _, err := os.Stat("./tern.conf"); err == nil {
			cliOptions.configPaths = append(cliOptions.configPaths, "./tern.conf")
		}
	}

	for _, configFile := range cliOptions.configPaths {
		err := appendConfigFromFile(config, configFile)
		if err != nil {
			return nil, err
		}
	}

	// If no migrations path was set in CLI argument look in environment.
	if cliOptions.migrationsPath == "" {
		cliOptions.migrationsPath = os.Getenv("TERN_MIGRATIONS")
	}

	// If no migrations path was set in CLI argument or environment use default.
	if cliOptions.migrationsPath == "" {
		cliOptions.migrationsPath = "."
	}

	err := appendConfigFromCLIArgs(config)
	if err != nil {
		return nil, err
	}

	// Set PG* variables from CLI args and config before parsing conn string
	for key, value := range config.PGEnvvars {
		if err := os.Setenv(key, value); err != nil {
			return nil, fmt.Errorf("error setting PostgreSQL environment variables from config: %s: %w", key, err)
		}
	}

	if connConfig, err := pgx.ParseConfig(config.ConnString); err == nil {
		config.ConnConfig = *connConfig
	} else {
		return nil, err
	}

	if config.SSHConnConfig.User == "" {
		user, err := user.Current()
		if err != nil {
			return nil, err
		}
		config.SSHConnConfig.User = user.Username
	}

	if config.SSHConnConfig.Port == "" {
		// Use SSH port by number instead of name because ":ssh" instead of ":22" doesn't work with the knownhosts package.
		// It would return: ssh: handshake failed: knownhosts: key is unknown
		config.SSHConnConfig.Port = "22"
	}

	if config.ConnConfig.RuntimeParams["application_name"] == "" {
		config.ConnConfig.RuntimeParams["application_name"] = "tern"
	}

	return config, nil
}

func appendConfigFromFile(config *Config, path string) error {
	fileBytes, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	confTemplate, err := template.New("conf").Funcs(sprig.TxtFuncMap()).Parse(string(fileBytes))
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	err = confTemplate.Execute(&buf, map[string]interface{}{})
	if err != nil {
		return err
	}

	file, err := ini.Load(&buf)
	if err != nil {
		return err
	}

	if connString, ok := file.Get("database", "conn_string"); ok {
		config.ConnString = connString
		if _, err := pgx.ParseConfig(connString); err != nil {
			return fmt.Errorf("error while parsing conn_string property: %w", err)
		}
	}

	if host, ok := file.Get("database", "host"); ok {
		config.PGEnvvars["PGHOST"] = host
	}

	// For backwards compatibility if host isn't set look for socket.
	if config.PGEnvvars["PGHOST"] == "" {
		if socket, ok := file.Get("database", "socket"); ok {
			config.PGEnvvars["PGHOST"] = socket
		}
	}

	if p, ok := file.Get("database", "port"); ok {
		_, err := strconv.ParseUint(p, 10, 16)
		if err != nil {
			return err
		}
		config.PGEnvvars["PGPORT"] = p
	}

	if database, ok := file.Get("database", "database"); ok {
		config.PGEnvvars["PGDATABASE"] = database
	}

	if user, ok := file.Get("database", "user"); ok {
		config.PGEnvvars["PGUSER"] = user
	}
	if password, ok := file.Get("database", "password"); ok {
		config.PGEnvvars["PGPASSWORD"] = password
	}

	if vt, ok := file.Get("database", "version_table"); ok {
		config.VersionTable = vt
	}

	if sslmode, ok := file.Get("database", "sslmode"); ok {
		config.PGEnvvars["PGSSLMODE"] = sslmode
	}

	if sslrootcert, ok := file.Get("database", "sslrootcert"); ok {
		config.PGEnvvars["PGSSLROOTCERT"] = sslrootcert
	}

	for key, value := range file["data"] {
		config.Data[key] = value
	}

	if host, ok := file.Get("ssh-tunnel", "host"); ok {
		config.SSHConnConfig.Host = host
	}

	if port, ok := file.Get("ssh-tunnel", "port"); ok {
		config.SSHConnConfig.Port = port
	}

	if user, ok := file.Get("ssh-tunnel", "user"); ok {
		config.SSHConnConfig.User = user
	}

	if password, ok := file.Get("ssh-tunnel", "password"); ok {
		config.SSHConnConfig.Password = password
	}

	if keyfile, ok := file.Get("ssh-tunnel", "keyfile"); ok {
		config.SSHConnConfig.KeyFile = keyfile
	}

	if passphrase, ok := file.Get("ssh-tunnel", "passphrase"); ok {
		config.SSHConnConfig.Passphrase = passphrase
	}
	return nil
}

func appendConfigFromCLIArgs(config *Config) error {
	if cliOptions.connString != "" {
		config.ConnString = cliOptions.connString
		if _, err := pgx.ParseConfig(cliOptions.connString); err != nil {
			return fmt.Errorf("error while parsing conn-string argument: %w", err)
		}
	}

	if cliOptions.host != "" {
		config.PGEnvvars["PGHOST"] = cliOptions.host
	}
	if cliOptions.port != 0 {
		config.PGEnvvars["PGPORT"] = strconv.FormatUint(uint64(cliOptions.port), 10)
	}
	if cliOptions.database != "" {
		config.PGEnvvars["PGDATABASE"] = cliOptions.database
	}
	if cliOptions.user != "" {
		config.PGEnvvars["PGUSER"] = cliOptions.user
	}
	if cliOptions.password != "" {
		config.PGEnvvars["PGPASSWORD"] = cliOptions.password
	}
	if cliOptions.sslmode != "" {
		config.PGEnvvars["PGSSLMODE"] = cliOptions.sslmode
	}
	if cliOptions.sslrootcert != "" {
		config.PGEnvvars["PGSSLROOTCERT"] = cliOptions.sslrootcert
	}
	if cliOptions.versionTable != "" {
		config.VersionTable = cliOptions.versionTable
	}

	if cliOptions.sshHost != "" {
		config.SSHConnConfig.Host = cliOptions.sshHost
	}
	if cliOptions.sshPort != "" {
		config.SSHConnConfig.Port = cliOptions.sshPort
	}
	if cliOptions.sshUser != "" {
		config.SSHConnConfig.User = cliOptions.sshUser
	}
	if cliOptions.sshPassword != "" {
		config.SSHConnConfig.Password = cliOptions.sshPassword
	}
	if cliOptions.sshKeyFile != "" {
		config.SSHConnConfig.KeyFile = cliOptions.sshKeyFile
	}
	if cliOptions.sshPassphrase != "" {
		config.SSHConnConfig.Passphrase = cliOptions.sshPassphrase
	}

	return nil
}

func PrintMigrations(cmd *cobra.Command, args []string) {

	ctx := context.Background()

	config, err := LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config:\n  %v\n", err)
		os.Exit(1)
	}

	err = config.Validate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid config:\n  %v\n", err)
		os.Exit(1)
	}
	var migrator *migrate.Migrator
	var currentVersion int32
	if cliOptions.currentVersion == "from_db" {
		// we need a db connection to get current version
		conn, err := config.Connect(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error connecting to database:\n  %v\n", err)
			os.Exit(1)
		}
		migrator, err = migrate.NewMigrator(ctx, conn, config.VersionTable)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing migrator:\n  %v\n", err)
			os.Exit(1)
		}
		currentVersion, err = migrator.GetCurrentVersion(ctx)
	} else {
		n, err := strconv.ParseInt(cliOptions.currentVersion, 10, 32)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Bad current version:\n  %v\n", err)
			os.Exit(1)
		}
		currentVersion = int32(n)

		migrator, err = migrate.NewMigrator(ctx, nil, config.VersionTable)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing migrator:\n  %v\n", err)
			os.Exit(1)
		}
	}

	migrator.Data = config.Data

	err = migrator.LoadMigrations(os.DirFS(cliOptions.migrationsPath))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading migrations:\n %v\n", err)
		os.Exit(1)
	}

	if len(migrator.Migrations) == 0 {
		fmt.Fprintln(os.Stderr, "No migrations found")
		os.Exit(1)
	}

	maxDestination := int32(len(migrator.Migrations))
	destination := mustParseDestination(cliOptions.destinationVersion, currentVersion, maxDestination)

	plan, err := PlanMigration(migrator, currentVersion, destination)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error planning migrations:\n  %v\n", err)
		os.Exit(1)
	}

	var out *os.File
	if cliOptions.outputFile == "" {
		out = os.Stdout
	} else {
		var err error
		out, err = os.Create(cliOptions.outputFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer out.Close()
	}

	printMigrationsTemplate := template.Must(template.New("print-migrations").Parse(
		`-- This file was generated by tern print-migrations v{{ .Version }}.
-- Migrating {{ .Plan.DirectionName }} from {{ .Plan.CurrentVersion }} to {{ .Plan.TargetVersion}}

{{range .Plan.Migrations -}}
-- {{ .Name }}
{{ if .SQL }}{{ .SQL }}{{ else }}-- empty migration{{ end }}

{{end }}
`))
	err = printMigrationsTemplate.Execute(out, map[string]any{
		"Version":      VERSION,
		"VersionTable": config.VersionTable,
		"Migrations":   plan.Migrations,
		"Plan":         plan,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error generating migration script:", err)
		os.Exit(1)
	}
}

type MigrationStep struct {
	migrate.Migration
	SQL      string
	Sequence interface{}
}

type MigrationPlan struct {
	CurrentVersion int32
	TargetVersion  int32
	Direction      int32
	DirectionName  string
	Migrations     []*MigrationStep
}

func PlanMigration(migrator *migrate.Migrator, currentVersion, targetVersion int32) (*MigrationPlan, error) {
	if targetVersion < 0 || int32(len(migrator.Migrations)) < targetVersion {
		errMsg := fmt.Sprintf("destination version %d is outside the valid versions of 0 to %d", targetVersion, len(migrator.Migrations))
		return nil, migrate.BadVersionError(errMsg)
	}

	if currentVersion < 0 || currentVersion > int32(len(migrator.Migrations)) {
		errMsg := fmt.Sprintf("current version %d is outside the valid versions of 0 to %d", currentVersion, len(migrator.Migrations))
		return nil, migrate.BadVersionError(errMsg)
	}

	var direction int32
	var directionName string
	if currentVersion < targetVersion {
		direction = 1
		directionName = "up"
	} else {
		direction = -1
		directionName = "down"
	}

	plan := MigrationPlan{
		CurrentVersion: currentVersion,
		TargetVersion:  targetVersion,
		Direction:      direction,
		DirectionName:  directionName,
	}

	for currentVersion != targetVersion {
		var current *migrate.Migration
		var sql string
		var sequence int32
		if direction == 1 {
			current = migrator.Migrations[currentVersion]
			sequence = current.Sequence
			sql = current.UpSQL

		} else {
			current = migrator.Migrations[currentVersion-1]
			sequence = current.Sequence - 1
			sql = current.DownSQL
		}

		plan.Migrations = append(plan.Migrations,
			&MigrationStep{
				Migration: *current,
				SQL:       sql,
				Sequence:  sequence,
			})

		currentVersion = currentVersion + direction
	}

	return &plan, nil
}

// mustParseDestination parses the destination argument and takes into account special syntax like
//
//   - 'last' (number of migration)
//   - '+N'   (current version + N)
//   - '-N'   (current version -N)
//
// calls os.Exit() on parse error
func mustParseDestination(destinationArg string, currentVersion int32, maxDestination int32) int32 {
	mustParseDestination := func(d string) int32 {
		var n int64
		n, err := strconv.ParseInt(d, 10, 32)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Bad destination:\n  %v\n", err)
			os.Exit(1)
		}
		return int32(n)
	}

	var destination int32
	if destinationArg == "last" {
		destination = maxDestination
	} else if len(destinationArg) >= 2 && destinationArg[0] == '-' {
		destination = currentVersion - mustParseDestination(destinationArg[1:])
	} else if len(destinationArg) >= 2 && destinationArg[0] == '+' {
		destination = currentVersion + mustParseDestination(destinationArg[1:])
	} else {
		destination = mustParseDestination(destinationArg)
	}
	return destination
}
