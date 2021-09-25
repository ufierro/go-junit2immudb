package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/jedib0t/go-pretty/v6/table"

	"github.com/codenotary/immudb/pkg/api/schema"
	"github.com/codenotary/immudb/pkg/client"
	"github.com/joshdk/go-junit"
)

func marshalWrapper(v interface{}) []byte {
	r, err := json.Marshal(v)
	if err != nil {
		log.Fatal(err.Error())
	}
	return r
}

func primeImmudb(ctx context.Context, client client.ImmuClient, parsed []junit.Suite) {
	relationsTableStatement := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (og_name VARCHAR[100], modified_name VARCHAR[100], PRIMARY KEY og_name)", nameRelationTable)
	_, err := client.SQLExec(ctx, relationsTableStatement, nil)
	if err != nil {
		log.Fatalf("Error creating relations table: %s", err.Error())
	}
}

func unBlob(col string, blob *schema.SQLValue) map[string]interface{} {
	var unBlobbed map[string]interface{}
	switch col {
	case "properties":
		json.Unmarshal(blob.GetBs(), &unBlobbed)
		return unBlobbed
	case "status":
		res := make(map[string]interface{})
		res[col] = blob.GetN()
		return res
	default:
		err := json.Unmarshal(blob.GetBs(), &unBlobbed)
		if err != nil {
			log.Fatal(err.Error())
		}
		return unBlobbed
	}

}

func printResults(results []Result) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"ID", "Name", "Status", "Duration", "Error", "Message", "Stdout", "Stderr", "Classname", "Properties"})
	for _, x := range results {
		//TODO: Split this into separate fields, convert everything to string or integer
		t.AppendRow(table.Row{x})
	}
	t.Render()
}
