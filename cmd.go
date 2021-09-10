package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/codenotary/immudb/pkg/api/schema"
	immuclient "github.com/codenotary/immudb/pkg/client"
	"github.com/joshdk/go-junit"
	"google.golang.org/grpc/metadata"
)

var config struct {
	hostname string
	port     int
	username string
	password string
	database string
	filename string
}

func initConfig() {
	flag.StringVar(&config.hostname, "hostname", "", "Hostname or IP address for immudb")
	flag.IntVar(&config.port, "port", 3322, "Port number of immudb server")
	flag.StringVar(&config.username, "username", "immudb", "Username for authenticating to immudb")
	flag.StringVar(&config.password, "password", "immudb", "Password for authenticating to immudb")
	flag.StringVar(&config.database, "database", "defaultdb", "Name of the database to use")
	flag.StringVar(&config.filename, "filename", "junit.xml", "The name of file, accepts comma separated values")
	flag.Parse()
}

func parseFiles() ([]junit.Suite, error) {
	// check if file list or single file
	var cError error
	files := strings.Split(config.filename, ",")
	if len(files) == 0 {
		log.Fatalf("No file provided")
	}
	if len(files) > 1 {
		parsedResponse, err := junit.IngestFiles(files)
		if err != nil {
			cError = err
		}
		return parsedResponse, cError
	} else {
		parsedResponse, err := junit.IngestFile(files[0])
		if err != nil {
			cError = err
		}
		return parsedResponse, cError
	}

}

func testSuiteToImmudb(parsed []junit.Suite) {
	opts := immuclient.DefaultOptions().WithAddress(config.hostname).WithPort(config.port)
	client, err := immuclient.NewImmuClient(opts)
	if err != nil {
		log.Fatalln("Failed to connect. Reason:", err)
	}

	ctx := context.Background()

	login, err := client.Login(ctx, []byte(config.username), []byte(config.password))
	if err != nil {
		log.Fatalln("Failed to login. Reason:", err.Error())
	}
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", login.GetToken()))

	udr, err := client.UseDatabase(ctx, &schema.Database{DatabaseName: config.database})
	if err != nil {
		log.Fatalln("Failed to use the database. Reason:", err)
	}
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", udr.GetToken()))
	for _, s := range parsed {
		// The following relations should hold true.
		//   Error == nil && (Status == Passed || Status == Skipped)
		//   Error != nil && (Status == Failed || Status == Error)
		log.Printf("Executing 'create table if not exists' for %s", s.Name)
		_, err := client.SQLExec(ctx, fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id INTEGER AUTO_INCREMENT, name VARCHAR NOT NULL, classname VARCHAR, duration BLOB, status BLOB, message VARCHAR, error BLOB, properties VARCHAR, systemout VARCHAR[256], systemerr VARCHAR[256], PRIMARY KEY id);", s.Name), nil)
		if err != nil {
			log.Fatalf("Error creating table %s", s.Name)
		}
		_, err = client.SQLExec(ctx, fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s_runs (id INTEGER AUTO_INCREMENT, name VARCHAR, package VARCHAR, properties BLOB, tests BLOB, suites BLOB, systemout VARCHAR[256], systemerr VARCHAR[256], totals BLOB, PRIMARY KEY id)", s.Name), nil)
		if err != nil {
			log.Println(err.Error())
			log.Fatalf("Error creating table %s", s.Name)
		}
		t, err := json.Marshal(s.Totals)
		if err != nil {
			log.Fatal("Error marshaling parsed response from junit file")
		}
		_, err = client.SQLExec(ctx, fmt.Sprintf("INSERT INTO %s_runs (name, package,totals) VALUES (@name, @package, @totals)", s.Name), map[string]interface{}{"name": s.Name, "package": s.Package, "totals": t})
		if err != nil {
			log.Fatal("Error inserting into database")
		}
		for _, t := range s.Tests {
			log.Println(t.Name)
			duration, _ := json.Marshal(t.Duration)
			tError, _ := json.Marshal(t.Error)
			status, _ := json.Marshal(t.Status)
			_, err = client.SQLExec(ctx, fmt.Sprintf("INSERT INTO %s (name, classname, duration, status, message, error) VALUES (@name, @classname, @duration, @status, @message, @error)", s.Name), map[string]interface{}{"name": t.Name, "classname": t.Classname, "duration": duration, "status": status, "message": t.Message, "error": tError})
			if err != nil {
				log.Fatalf("Error inserting test results: %s", err.Error())
			}
		}
	}

}

func main() {
	initConfig()
	response, err := parseFiles()
	if err != nil {
		log.Fatalf("Failed to parse file, error: %s", config.filename)
	}
	testSuiteToImmudb(response)

}
