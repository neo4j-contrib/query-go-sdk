// Package main demonstrates using the Neo4j Query API Go SDK.
// It shows three ways to consume query results using the transformer pattern,
// which mirrors the Neo4j Go driver's EagerResultTransformer approach.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	query "github.com/neo4j-contrib/query-go-sdk"
)

// Actor is an example domain type we map query results into.
type Actor struct {
	Name  string
	Title string
	Roles []string
}

func main() {
	username := "neo4j"
	password := "password"

	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	handler := slog.NewTextHandler(os.Stderr, opts)
	customLogger := slog.New(handler)

	client, err := query.NewClient(
		query.WithBaseURL("http://localhost:7474"),
		query.WithTimeout(120*time.Second),
		query.WithLogger(customLogger),
		query.WithBasicAuth(username, password),
		query.WithMaxResponseSize(10*1024*1024), // 10 mb max size
	)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// =========================================================================
	// Example 1 — EagerResultTransformer
	//
	// Collects all rows into an EagerResult with named Record access.
	// Mirrors neo4j.EagerResultTransformer in the Go driver.
	// Use this when you want to inspect rows without mapping to a struct.
	// =========================================================================
	fmt.Println("=== Example 1: EagerResultTransformer ===")

	cypher := `MATCH (p:Person)-[a:ACTED_IN]-(m:Movie {title:'The Matrix'}) RETURN p.name, m.title, a.roles`

	result, err := query.WithTransformer(client.Query, ctx, cypher, nil, query.EagerResultTransformer)
	if err != nil {
		handleQueryError(err)
		log.Fatalf("Failed to execute query: %v", err)
	}

	fmt.Printf("Keys: %v\n", result.Keys)
	fmt.Printf("Rows returned: %d\n", len(result.Records))

	for _, rec := range result.Records {
		name, _ := rec.GetString("p.name")
		title, _ := rec.GetString("m.title")
		rawRoles, _ := rec.GetList("a.roles")
		roles, _ := query.StringList(rawRoles)
		fmt.Printf("  %s in %q — roles: %v\n", name, title, roles)
	}

	if result.HasWarnings() {
		fmt.Println("Warnings:")
		for _, w := range result.Warnings() {
			fmt.Printf("  [%s] %s\n", w.Code, w.Title)
		}
	}

	// =========================================================================
	// Example 2 — Collect
	//
	// Maps each row directly to a domain struct using a mapping function.
	// Returns []Actor — no type assertions needed at the call site.
	// =========================================================================
	fmt.Println("\n=== Example 2: Collect ===")

	actors, err := query.WithTransformer(client.Query, ctx, cypher, nil,
		query.Collect(func(rec *query.Record) (Actor, error) {
			name, _ := rec.GetString("p.name")
			title, _ := rec.GetString("m.title")
			rawRoles, _ := rec.GetList("a.roles")
			roles, _ := query.StringList(rawRoles)
			return Actor{
				Name:  name,
				Title: title,
				Roles: roles,
			}, nil
		}),
	)
	if err != nil {
		handleQueryError(err)
		log.Fatalf("Failed to execute query: %v", err)
	}

	for _, actor := range actors {
		fmt.Printf("  %s → %v\n", actor.Name, actor.Roles)
	}

	// =========================================================================
	// Example 3 — Single
	//
	// Expects exactly one row and maps it to a struct.
	// Returns an error if zero or more than one row is returned.
	// =========================================================================
	fmt.Println("\n=== Example 3: Single ===")

	singleCypher := `MATCH (m:Movie {title:'The Matrix'}) RETURN m.title, m.released`

	type Movie struct {
		Title    string
		Released int64
	}

	movie, err := query.WithTransformer(client.Query, ctx, singleCypher, nil,
		query.Single(func(rec *query.Record) (Movie, error) {
			title, _ := rec.GetString("m.title")
			released, _ := rec.GetInt64("m.released")
			return Movie{Title: title, Released: released}, nil
		}),
	)
	if err != nil {
		handleQueryError(err)
		log.Fatalf("Failed to execute query: %v", err)
	}

	fmt.Printf("  %s (%d)\n", movie.Title, movie.Released)

	// =========================================================================
	// Example 4 — Whole nodes (RETURN n not n.property)
	//
	// When Cypher returns whole graph entities, use GetNode / GetRelationship.
	// =========================================================================
	fmt.Println("\n=== Example 4: Whole nodes ===")

	nodeCypher := `MATCH (p:Person)-[a:ACTED_IN]-(m:Movie {title:'The Matrix'}) RETURN p, a, m LIMIT 2`

	nodeResult, err := query.WithTransformer(client.Query, ctx, nodeCypher, nil, query.EagerResultTransformer)
	if err != nil {
		handleQueryError(err)
		log.Fatalf("Failed to execute query: %v", err)
	}

	for _, rec := range nodeResult.Records {
		person, _ := rec.GetNode("p")
		movie, _ := rec.GetNode("m")
		rel, _ := rec.GetRelationship("a")
		if person != nil && movie != nil && rel != nil {
			fmt.Printf("  (%v)-[%s]->(%v)\n",
				person.Properties["name"],
				rel.Type,
				movie.Properties["title"],
			)
		}
	}

	fmt.Println("\n✓ All examples completed successfully!")
}

// handleQueryError inspects a Neo4j query error and prints detail.
// Uses query.QueryErrors — no internal package import needed.
func handleQueryError(err error) {
	var qErr *query.QueryErrors
	if errors.As(err, &qErr) {
		fmt.Fprintln(os.Stderr, "Neo4j query error(s):")
		for _, e := range qErr.Errors {
			fmt.Fprintf(os.Stderr, "  [%s] %s: %s\n", e.Classification(), e.Title(), e.Message)
		}
	}
}
