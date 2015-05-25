package pgx_test

import (
	"crypto/tls"
	"github.com/jackc/pgx"
)

var defaultConnConfig *pgx.ConnConfig = &pgx.ConnConfig{Host: "/var/run/postgresql", User: "pgx_md5", Password: "secret", Database: "pgx_test"}

// To skip tests for specific connection / authentication types set that connection param to nil
var tcpConnConfig *pgx.ConnConfig = &pgx.ConnConfig{Host: "127.0.0.1", User: "pgx_md5", Password: "secret", Database: "pgx_test"}
var unixSocketConnConfig *pgx.ConnConfig = &pgx.ConnConfig{Host: "/var/run/postgresql", User: "pgx_none", Database: "pgx_test"}
var md5ConnConfig *pgx.ConnConfig = &pgx.ConnConfig{Host: "/var/run/postgresql", User: "pgx_md5", Password: "secret", Database: "pgx_test"}
var tlsConnConfig *pgx.ConnConfig = &pgx.ConnConfig{Host: "127.0.0.1", User: "pgx_md5", Password: "secret", Database: "pgx_test", TLSConfig: &tls.Config{InsecureSkipVerify: true}}
var plainPasswordConnConfig *pgx.ConnConfig = &pgx.ConnConfig{Host: "/var/run/postgresql", User: "pgx_pw", Password: "secret", Database: "pgx_test"}
var noPasswordConnConfig *pgx.ConnConfig = &pgx.ConnConfig{Host: "/var/run/postgresql", User: "pgx_none", Database: "pgx_test"}
var invalidUserConnConfig *pgx.ConnConfig = &pgx.ConnConfig{Host: "/var/run/postgresql", User: "invalid", Database: "pgx_test"}
var customDialerConnConfig = &pgx.ConnConfig{Host: "127.0.0.1", User: "pgx_md5", Password: "secret", Database: "pgx_test"}

// var tcpConnConfig *pgx.ConnConfig = &pgx.ConnConfig{Host: "127.0.0.1", User: "pgx_md5", Password: "secret", Database: "pgx_test"}
// var unixSocketConnConfig *pgx.ConnConfig = &pgx.ConnConfig{Host: "/private/tmp/.s.PGSQL.5432", User: "pgx_none", Database: "pgx_test"}
// var md5ConnConfig *pgx.ConnConfig = &pgx.ConnConfig{Host: "127.0.0.1", User: "pgx_md5", Password: "secret", Database: "pgx_test"}
// var plainPasswordConnConfig *pgx.ConnConfig = &pgx.ConnConfig{Host: "127.0.0.1", User: "pgx_pw", Password: "secret", Database: "pgx_test"}
// var noPasswordConnConfig *pgx.ConnConfig = &pgx.ConnConfig{Host: "127.0.0.1", User: "pgx_none", Database: "pgx_test"}
// var invalidUserConnConfig *pgx.ConnConfig = &pgx.ConnConfig{Host: "127.0.0.1", User: "invalid", Database: "pgx_test"}
