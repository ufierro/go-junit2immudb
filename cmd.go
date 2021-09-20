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

var nameRelationTable = "NM_REL"

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

var unmarshallErr = "Error unmarshalling value"
var marshalErr = "Error marshalling value"

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

func getOriginalName(ctx context.Context, client immuclient.ImmuClient, suiteName string) string {
	q := fmt.Sprintf("SELECT modified_name FROM %s WHERE OG_NAME = '%s' ", nameRelationTable, suiteName)
	nameQueryResult, err := client.SQLQuery(ctx, q, nil, false)
	originalName := ""
	if err != nil {
		// this means the query failed, it's not an empty resultset
		log.Fatalf("Error executing query: %s", q)
	}
	for _, rows := range nameQueryResult.Rows {
		originalName = rows.Values[0].GetS()
	}
	log.Println(originalName)
	return originalName
}

func testSuiteToImmudb(ctx context.Context, parsed []junit.Suite, client immuclient.ImmuClient) {
	for _, s := range parsed {
		if s.Name == "" {
			log.Printf("no test suite name found in %s , consider adding a name for ease of usage", config.filename)
			s.Name = "generic_testsuite"
		}
		log.Printf("Processing suite: %s", s.Name)
		log.Printf("Executing: 'create table if not exists' for suite %s, characters not allowed in table names will be replaced by underscores", s.Name)
		reg := regexp.MustCompile("[^a-zA-Z0-9]+")
		relationsTableStatement := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (og_name VARCHAR[100], modified_name VARCHAR[100], PRIMARY KEY og_name)", nameRelationTable)
		_, err := client.SQLExec(ctx, relationsTableStatement, nil)
		if err != nil {
			log.Fatalf("Error creating relations table: %s", err.Error())
		}
		createStatement := ""
		nameToUse := getOriginalName(ctx, client, s.Name)
		if nameToUse == "" {
			processedString := reg.ReplaceAllString(s.Name, "")
			formattedName := strings.ReplaceAll(processedString, " ", "_")
			createStatement = fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id INTEGER AUTO_INCREMENT, name VARCHAR, classname VARCHAR, duration BLOB, status BLOB, message VARCHAR, error BLOB, properties BLOB, systemout VARCHAR, systemerr VARCHAR, PRIMARY KEY id)", formattedName)
			relationStatement := fmt.Sprintf("INSERT INTO %s (og_name, modified_name) VALUES (@og_name, @modified_name)", nameRelationTable)
			_, err := client.SQLExec(ctx, relationStatement, map[string]interface{}{"og_name": s.Name, "modified_name": formattedName})
			if err != nil {
				log.Fatalf("error creating table relationship for %s", relationStatement)
			}
			nameToUse = formattedName
		} else {
			createStatement = fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id INTEGER AUTO_INCREMENT, name VARCHAR, classname VARCHAR, duration BLOB, status BLOB, message VARCHAR, error BLOB, properties BLOB, systemout VARCHAR, systemerr VARCHAR, PRIMARY KEY id)", nameToUse)
		}
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
			log.Fatal(unmarshallErr)
		}
		p, err := json.Marshal(s.Package)
		if err != nil {
			log.Fatal("Error marshaling parsed response from junit file")
		}
		props, err := json.Marshal(s.Properties)
		if err != nil {
			log.Fatal(unmarshallErr)
		}
		t, err := json.Marshal(s.Tests)
		if err != nil {
			log.Fatal(unmarshallErr)
		}
		suites, err := json.Marshal(s.Suites)
		if err != nil {
			log.Fatal(unmarshallErr)
		}
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
			if err != nil {
				log.Fatal(marshalErr)
			}
			p, err := json.Marshal(t.Properties)
			if err != nil {
				log.Fatal(marshalErr)
			}
			s, err := json.Marshal(t.Status)
			if err != nil {
				log.Fatal(marshalErr)
			}
			e, err := json.Marshal(t.Error)
			if err != nil {
				log.Fatalf("Error marshalling parser response for %s", t.Name)
			}
			// log.Println(fmt.Sprintf("Processing test case: %s", t.Name))
			_, err = client.SQLExec(ctx, fmt.Sprintf("INSERT INTO %s (name, classname, duration, status, message, error, properties, systemout, systemerr) VALUES (@name, @classname, @duration, @status, @message, @error, @properties, @systemout, @systemerr)", nameToUse), map[string]interface{}{"name": t.Name, "classname": t.Classname, "duration": d, "status": s, "message": t.Message, "error": e, "properties": p, "systemout": t.SystemOut, "systemerr": t.SystemErr})
			if err != nil {
				log.Fatalf("Error inserting test results: %s", err.Error())
			}
		}
	}

}

func readResults(ctx context.Context, client immuclient.ImmuClient) {
	tableList, err := client.ListTables(ctx)
	if err != nil {
		log.Fatalf("Error reading system tables")
	}
	var tableToLookFor string
	tableToUse := ""

	if config.readSummary {
		tableToLookFor = config.suiteTableName
	} else {
		tableToLookFor = config.readPrefix
	}
	for _, r := range tableList.Rows {
		// (r.Columns)
		row := make([]string, len(r.Values))
		for i, v := range r.Values {
			row[i] = schema.RenderValue(v.Value)
		}
		log.Printf("Configuration: Summary %t, Suite Table Name:%s", config.readSummary, config.suiteTableName)
		for t := range row {
			log.Printf("Found table %s", row[t])
			if strings.Contains(strings.ReplaceAll(row[t], `"`, ""), tableToLookFor) {
				tableToUse = strings.ReplaceAll(row[t], `"`, "")
				break
			}
		}
	}
	if tableToUse == "" {
		tableToUse = config.readPrefix
	}
	q := fmt.Sprintf("SELECT * FROM %s", tableToUse)
	log.Println(q)
	summaryResults, err := client.SQLQuery(ctx, q, nil, false)
	if err != nil {
		log.Println(err.Error())
		log.Fatalf("Failed to read table %s", tableToUse)
	}
	colsLen := len(summaryResults.Columns)
	for _, sumInfo := range summaryResults.Rows {
		for i := 0; i <= colsLen-1; i++ {
			currentCol := strings.Split(strings.ReplaceAll(sumInfo.Columns[i], `)`, ""), `.`)[len(strings.Split(sumInfo.Columns[i], `.`))-1]
			currentVal := sumInfo.Values[i]
			switch currentCol {
			case "properties":
				var props map[string]string
				err := json.Unmarshal(currentVal.GetBs(), &props)
				if err != nil {
					log.Fatal("error marshalling value")
				}
				log.Printf("------%s-------", currentCol)
				for k, v := range props {
					log.Printf("%s:%s", k, v)
				}
			default:
				log.Printf("------%s-------\n------%s-------", currentCol, currentVal)

			}

		}

	}
}

func main() {
	initConfig()
	client, ctx := initDbSession()
	if !config.readResults {
		response, err := parseFiles()
		if err != nil {
			log.Fatalf("Failed to parse file, error: %s", config.filename)
		}
		testSuiteToImmudb(ctx, response, client)

	} else {
		readResults(ctx, client)
	}
}
