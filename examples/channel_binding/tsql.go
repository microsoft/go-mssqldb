package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	// mssqldb "github.com/denisenkom/go-mssqldb"
	// "github.com/denisenkom/go-mssqldb/msdsn"
	"github.com/google/uuid"
	mssqldb "github.com/microsoft/go-mssqldb"
	"github.com/microsoft/go-mssqldb/msdsn"
	_ "github.com/microsoft/go-mssqldb/integratedauth/krb5"
)

func main() {
	var (
		userid   = flag.String("U", "", "login_id")
		password = flag.String("P", "", "password")
		server   = flag.String("S", "localhost", "server_name[\\instance_name]")
		port     = flag.Uint64("p", 1433, "server port")
		keyLog   = flag.String("K", "tlslog.log", "path to sslkeylog file")
		database = flag.String("d", "", "db_name")
		spn      = flag.String("spn", "", "SPN")
		auth = flag.String("a", "ntlm", "Authentication method: ntlm or krb5")
		epa = flag.String("epa", "tls-unique", "EPA mode: tls-unique, tls-server-end-point")
	)
	flag.Parse()

	keyLogFile, err := os.OpenFile(*keyLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		log.Fatal("failed to open keylog file:", err)
	}
	defer keyLogFile.Close()


	cfg := msdsn.Config{
		User:           *userid,
		Database:       *database,
		Host:           *server,
		Port:           *port,
		Password:       *password,
		ChangePassword: "",
		AppName:        "go-mssqldb",
		ServerSPN: *spn,
		TLSConfig: &tls.Config{
			InsecureSkipVerify:          true, // adjust for your case
			ServerName:                  *server,
			KeyLogWriter:                keyLogFile,
			DynamicRecordSizingDisabled: true,
			MinVersion:                  tls.VersionTLS11,
			MaxVersion:                  tls.VersionTLS12,
		},
		Encryption:         msdsn.EncryptionRequired,

		Parameters: map[string]string{
			"authenticator": *auth,
			"krb5-credcachefile": "/tmp/krb5cc_719880",
			"krb5-configfile": "/etc/krb5.conf",
		},
		ProtocolParameters: map[string]interface{}{

		},
		Protocols: []string{
			"tcp",
		},
		Encoding: msdsn.EncodeParameters{
			Timezone:       time.UTC,
			GuidConversion: false,
		},
		DialTimeout: time.Second * 5,
		ConnTimeout: time.Second * 10,
		KeepAlive:   time.Second * 30,
		EpaMode: msdsn.EpaMode(*epa),
	}

	// if *spn != "" {
	// 	cfg.Parameters["authenticator"]	= "krb5"
	// 	// cfg.Parameters["krb5-credcachefile"]	= "/tmp/krb5cc_719880"
	// }

	activityid, uerr := uuid.NewRandom()
	if uerr == nil {
		cfg.ActivityID = activityid[:]
	}

	workstation, err := os.Hostname()
	if err == nil {
		cfg.Workstation = workstation
	}

	connector := mssqldb.NewConnectorConfig(cfg)
	// dsn := "server=" + *server + ";user id=" + *userid + ";password=" + *password + ";database=" + *database
	// connector,err = mssqldb.NewConnector(dsn)
	// if err != nil {
	// 	fmt.Println("failed to create connector: ", err.Error())
	// 	return
	// }

	_, err = connector.Connect(context.Background())
	if err != nil {
		fmt.Println("connector.Connect: ", err.Error())
		return
	}

	db := sql.OpenDB(connector)
	defer db.Close()

	// if err != nil {
	// 	fmt.Println("Cannot connect: ", err.Error())
	// 	return
	// }
	err = db.Ping()
	if err != nil {
		fmt.Println("Cannot connect: ", err.Error())
		return
	}
	r := bufio.NewReader(os.Stdin)
	for {
		_, err = os.Stdout.Write([]byte("> "))
		if err != nil {
			fmt.Println(err)
			return
		}
		cmd, err := r.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				fmt.Println()
				return
			}
			fmt.Println(err)
			return
		}
		err = exec(db, cmd)
		if err != nil {
			fmt.Println(err)
		}
	}
}
func exec(db *sql.DB, cmd string) error {
	rows, err := db.Query(cmd)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	if cols == nil {
		return nil
	}
	vals := make([]interface{}, len(cols))
	for i := 0; i < len(cols); i++ {
		vals[i] = new(interface{})
		if i != 0 {
			fmt.Print("\t")
		}
		fmt.Print(cols[i])
	}
	fmt.Println()
	for rows.Next() {
		err = rows.Scan(vals...)
		if err != nil {
			fmt.Println(err)
			continue
		}
		for i := 0; i < len(vals); i++ {
			if i != 0 {
				fmt.Print("\t")
			}
			printValue(vals[i].(*interface{}))
		}
		fmt.Println()

	}
	if rows.Err() != nil {
		return rows.Err()
	}
	return nil
}

func printValue(pval *interface{}) {
	switch v := (*pval).(type) {
	case nil:
		fmt.Print("NULL")
	case bool:
		if v {
			fmt.Print("1")
		} else {
			fmt.Print("0")
		}
	case []byte:
		fmt.Print(string(v))
	case time.Time:
		fmt.Print(v.Format("2006-01-02 15:04:05.999"))
	default:
		fmt.Print(v)
	}
}