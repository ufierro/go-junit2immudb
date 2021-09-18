package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/codenotary/immudb/pkg/api/schema"
	immuclient "github.com/codenotary/immudb/pkg/client"
	"github.com/joshdk/go-junit"
	"google.golang.org/grpc/metadata"
)

var config struct {
	hostname       string
	port           int
	username       string
	password       string
	database       string
	filename       string
	suiteTableName string
	readPrefix     string
	readResults    bool
	readSummary    bool

	limit int
}

func initConfig() {
	flag.StringVar(&config.hostname, "hostname", "localhost", "Hostname or IP address for immudb")
	flag.StringVar(&config.username, "username", "immudb", "Username for authenticating to immudb")
	flag.StringVar(&config.password, "password", "immudb", "Password for authenticating to immudb")
	flag.StringVar(&config.database, "database", "defaultdb", "Name of the database to use")
	flag.StringVar(&config.filename, "filename", "junit.xml", "The name of file, accepts comma separated values")
	flag.StringVar(&config.suiteTableName, "summary_tbl_name", "junit_suite_summary", "The name of table used for test suite summary, creates new one if it doesn't exist already")
	flag.IntVar(&config.port, "port", 3322, "Port number of immudb server")
	flag.BoolVar(&config.readResults, "read-results", false, "Read results from db, if specified write related flags are ignored")
	flag.BoolVar(&config.readSummary, "summary", false, "Read only summary results from db, if specified table name related flags are ignored")
	flag.StringVar(&config.readPrefix, "suite-prefix", "junit_", "The prefix for which tests will be read, queries table and obtains all test results")
	flag.IntVar(&config.limit, "limit", 10, "limit=N where N is the maximum number of test executions to display for a given test")
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

func initDbSession() (immuclient.ImmuClient, context.Context) {
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
	return client, ctx
}

func testSuiteToImmudb(parsed []junit.Suite, client immuclient.ImmuClient, ctx context.Context) {
	for _, s := range parsed {
		log.Printf("Processing suite: %s", s.Name)
		log.Printf("Executing: 'create table if not exists' for suite %s, characters not allowed in table names will be replaced by underscores", s.Name)
		reg, err := regexp.Compile("[^a-zA-Z0-9]+")
		if err != nil {
			log.Fatalf("Error removing stripping %s, consider renaming your test suite", s.Name)
		}
		processedString := reg.ReplaceAllString(s.Name, "")
		formattedName := strings.Replace(processedString, " ", "_", -1)
		createStatement := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id INTEGER AUTO_INCREMENT, name VARCHAR, classname VARCHAR, duration BLOB, status BLOB, message VARCHAR, error BLOB, properties BLOB, systemout VARCHAR, systemerr VARCHAR, PRIMARY KEY id)", formattedName)
		log.Println(createStatement)
		_, err = client.SQLExec(ctx, createStatement, nil)
		if err != nil {
			log.Println(err.Error())
			log.Fatalf("Error creating table %s", s.Name)
		}
		_, err = client.SQLExec(ctx, fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id INTEGER AUTO_INCREMENT, name VARCHAR, package BLOB, properties BLOB, tests BLOB, suites BLOB, systemout VARCHAR, systemerr VARCHAR, totals BLOB, PRIMARY KEY id)", config.suiteTableName), nil)
		if err != nil {
			log.Println(err.Error())
			log.Fatalf("Error creating table %s", s.Name)
		}
		if err != nil {
			log.Fatal("Error marshaling parsed response from junit file")
		}
		p, err := json.Marshal(s.Package)
		props, err := json.Marshal(s.Properties)
		t, err := json.Marshal(s.Tests)
		suites, err := json.Marshal(s.Suites)
		totals, err := json.Marshal(s.Totals)
		if err != nil {
			log.Fatal("Error marshalling parser response for test suite summary")
		}
		_, err = client.SQLExec(
			ctx, fmt.Sprintf("INSERT INTO %s (name, package, properties, tests, suites, systemout, systemerr, totals) VALUES (@name, @package, @properties, @tests, @suites, @systemout, @systemerr, @totals)",
				config.suiteTableName), map[string]interface{}{"name": s.Name, "package": p, "properties": props, "tests": t, "suites": suites, "systemout": s.SystemOut, "systemerr": s.SystemErr, "totals": totals})
		if err != nil {
			log.Println(err.Error())
			log.Fatal("Error inserting suite results into database")
		}
		for _, t := range s.Tests {
			d, err := json.Marshal(t.Duration)
			p, err := json.Marshal(t.Properties)
			s, err := json.Marshal(t.Status)
			e, err := json.Marshal(t.Error)
			if err != nil {
				log.Fatalf("Error marshalling parser response for %s", t.Name)
			}
			//log.Println(fmt.Sprintf("Processing test case: %s", t.Name))
			_, err = client.SQLExec(ctx, fmt.Sprintf("INSERT INTO %s (name, classname, duration, status, message, error, properties, systemout, systemerr) VALUES (@name, @classname, @duration, @status, @message, @error, @properties, @systemout, @systemerr)", formattedName), map[string]interface{}{"name": t.Name, "classname": t.Classname, "duration": d, "status": s, "message": t.Message, "error": e, "properties": p, "systemout": t.SystemOut, "systemerr": t.SystemErr})
			if err != nil {
				log.Fatalf("Error inserting test results: %s", err.Error())
			}
		}
	}

}

func readResults(client immuclient.ImmuClient, ctx context.Context) {
	tableList, err := client.ListTables(ctx)
	if err != nil {
		log.Fatalf("Error reading system tables")
	}
	var tableToUse string
	for _, r := range tableList.Rows {
		//junit_suite_summary is the name of the default summary table
		row := make([]string, len(r.Values))
		// if row.Values == config.suiteTableName {
		for i, v := range r.Values {
			row[i] = schema.RenderValue(v.Value)
		}
		log.Printf("Configuration: Summary %t, Suite Table Name:%s", config.readSummary, config.suiteTableName)
		for t := range row {
			log.Printf("Found table %s", row[t])
			if config.suiteTableName == strings.Replace(row[t], `"`, "", -1) {
				log.Printf("Using table %s", row[t])
				tableToUse = row[t]
				break
			} else {
				if strings.Contains(strings.Replace(row[t], `"`, "", -1), config.readPrefix) {
					tableToUse = row[t]
					break
				}
			}
		}

	}
	log.Println(tableToUse)
}

func main() {
	initConfig()
	client, ctx := initDbSession()
	if !config.readResults {
		response, err := parseFiles()
		if err != nil {
			log.Fatalf("Failed to parse file, error: %s", config.filename)
		}
		testSuiteToImmudb(response, client, ctx)

	} else {
		readResults(client, ctx)
	}

}
