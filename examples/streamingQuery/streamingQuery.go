// Package main demonstrates streaming query results with the Neo4j Query API Go SDK.
// Streaming decodes records incrementally as they arrive over the wire instead
// of buffering the entire response, which matters for large result sets.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	query "github.com/neo4j-contrib/query-go-sdk"
)

func main() {
	username := "neo4j"
	password := "password"

	client, err := query.NewClient(
		query.WithBaseURL("http://localhost:7474"),
		query.WithTimeout(120*time.Second),
		query.WithBasicAuth(username, password),
		query.WithStreamingSupport(true), // required for ExecuteStream
	)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	cypher := `MATCH (p:Person)-[a:ACTED_IN]-(m:Movie) RETURN p.name AS name, m.title AS title`

	result, err := client.Query.ExecuteStream(ctx, cypher, nil)
	if err != nil {
		handleQueryError(err)
		log.Fatalf("Failed to start streaming query: %v", err)
	}

	fmt.Printf("Fields: %v\n", result.Fields())

	count := 0
	for rec, err := range result.Records() {
		if err != nil {
			handleQueryError(err)
			log.Fatalf("Failed while streaming records: %v", err)
		}
		name, _ := rec.GetString("name")
		title, _ := rec.GetString("title")
		fmt.Printf("  %s in %q\n", name, title)
		count++
	}

	fmt.Printf("\nStreamed %d records\n", count)

	if summary := result.Summary(); summary != nil {
		fmt.Printf("Bookmarks: %v\n", summary.Bookmarks)
		fmt.Printf("Query type: %s\n", summary.QueryType)
		if summary.HasWarnings() {
			fmt.Println("Warnings:")
			for _, w := range summary.Warnings() {
				fmt.Printf("  [%s] %s\n", w.Code, w.Title)
			}
		}
	}
}

// handleQueryError inspects a Neo4j query error and prints detail.
func handleQueryError(err error) {
	var qErr *query.QueryErrors
	if errors.As(err, &qErr) {
		fmt.Println("Neo4j query error(s):")
		for _, e := range qErr.Errors {
			fmt.Printf("  [%s] %s: %s\n", e.Classification(), e.Title(), e.Message)
		}
	}
}
