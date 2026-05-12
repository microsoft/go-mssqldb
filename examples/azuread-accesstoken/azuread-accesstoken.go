package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	mssql "github.com/microsoft/go-mssqldb"
)

var (
	debug    = flag.Bool("debug", false, "enable debugging")
	server   = flag.String("server", "", "the database server")
	database = flag.String("database", "", "the database")
)

func main() {
	flag.Parse()

	if *debug {
		fmt.Printf(" server:%s\n", *server)
		fmt.Printf(" database:%s\n", *database)
	}

	if *server == "" {
		log.Fatal("Server name cannot be left empty")
	}

	if *database == "" {
		log.Fatal("Database name cannot be left empty")
	}

	connString := fmt.Sprintf("Server=%s;Database=%s", *server, *database)
	if *debug {
		fmt.Printf(" connString:%s\n", connString)
	}

	cred, err := azidentity.NewManagedIdentityCredential(nil)
	if err != nil {
		log.Fatal("Error creating managed identity credential:", err.Error())
	}
	tokenProvider := func() (string, error) {
		token, err := cred.GetToken(context.TODO(), policy.TokenRequestOptions{
			Scopes: []string{"https://database.windows.net//.default"},
		})
		return token.Token, err
	}

	connector, err := mssql.NewAccessTokenConnector(connString, tokenProvider)
	if err != nil {
		log.Fatal("Connector creation failed:", err.Error())
	}
	conn := sql.OpenDB(connector)
	defer conn.Close()

	row := conn.QueryRow("select 1, 'abc'")
	var somenumber int64
	var somechars string
	err = row.Scan(&somenumber, &somechars)
	if err != nil {
		log.Fatal("Scan failed:", err.Error())
	}
	fmt.Printf("somenumber:%d\n", somenumber)
	fmt.Printf("somechars:%s\n", somechars)

	fmt.Printf("bye\n")
}
