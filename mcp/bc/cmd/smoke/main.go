// Command smoke is a throwaway end-to-end check for the sgclient transport
// against the real public Sourcegraph instance. It is not part of the product
// surface (that is cmd/server) — it exists to prove Cycle 1 works against real
// code, and to feel the integration friction first-hand (analysis doc §2).
//
// Usage:
//
//	go run ./mcp/bc/cmd/smoke -q 'lang:go fmt.Errorf("%w"' -n 5
//	go run ./mcp/bc/cmd/smoke -file github.com/sourcegraph/sourcegraph -path README.md
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/sgclient"
	"github.com/alaneduardo/sg-mcp-bc-example/mcp/bc/internal/targeting"
)

const publicEndpoint = "https://sourcegraph.com/.api/graphql"

func main() {
	query := flag.String("q", "", "Sourcegraph search query to resolve into targets")
	maxRepos := flag.Int("n", 10, "max repos to return")
	fileRepo := flag.String("file", "", "repo to fetch a file from (use with -path)")
	filePath := flag.String("path", "", "file path to fetch")
	rev := flag.String("rev", "", "revision (default HEAD)")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := sgclient.New(publicEndpoint, sgclient.WithToken(os.Getenv("SRC_ACCESS_TOKEN")))

	switch {
	case *fileRepo != "" && *filePath != "":
		if err := runFetch(ctx, client, *fileRepo, *filePath, *rev); err != nil {
			fail(err)
		}
	case *query != "":
		if err := runSearch(ctx, client, *query, *maxRepos); err != nil {
			fail(err)
		}
	default:
		fmt.Fprintln(os.Stderr, "provide -q <query> or -file <repo> -path <path>")
		os.Exit(2)
	}
}

func runSearch(ctx context.Context, client *sgclient.Client, raw string, maxRepos int) error {
	q, err := targeting.NewQuery(raw)
	if err != nil {
		return err
	}
	targets, err := client.Search(ctx, q, maxRepos)
	if err != nil {
		return err
	}
	fmt.Printf("normalized_query: %s\n", q.String())
	fmt.Printf("total_repos: %d  truncated: %t\n\n", targets.TotalRepos, targets.Truncated)
	for _, t := range targets.Items {
		fmt.Printf("%-50s  %d occurrences\n", t.Repo, t.OccurrenceCount)
		for _, p := range t.SamplePaths {
			fmt.Printf("    %s\n", p)
		}
	}
	return nil
}

func runFetch(ctx context.Context, client *sgclient.Client, repo, path, rev string) error {
	fc, err := client.FetchFile(ctx, repo, path, rev)
	if err != nil {
		return err
	}
	fmt.Printf("%s@%s (%d bytes)\n\n%s\n", fc.Path, fc.RevResolved, fc.SizeBytes, fc.Content)
	return nil
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
