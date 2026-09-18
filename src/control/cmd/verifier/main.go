// A read-only process: emits a verdict on stdout; cannot append to the ledger.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	c "github.com/someshwaranM/forge-tyr/src/control"
	"os"
)

func main() {
	run := flag.String("run-id", "", "frozen run")
	contract := flag.String("contract", "contracts/checkout-slo.json", "contract source")
	queries := flag.String("queries", "elastic/queries", "query directory")
	summary := flag.String("summary", "", "generator JSON summary")
	checkScope := flag.Bool("check-scope", false, "check actual read-only privileges without evaluating a run")
	flag.Parse()
	client := c.Elastic{Endpoint: os.Getenv("ELASTIC_CLOUD_ENDPOINT"), Key: os.Getenv("ELASTIC_VERIFIER_API_KEY")}
	if *checkScope {
		if err := c.VerifierScope(client); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("Verifier credential: reads allowed, evidence/control writes forbidden")
		return
	}
	var s c.Summary
	b, err := os.ReadFile(*summary)
	if err == nil {
		err = json.Unmarshal(b, &s)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	v, err := c.Verify(client, *run, *contract, *queries, s)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	e := json.NewEncoder(os.Stdout)
	e.SetIndent("", "  ")
	if err = e.Encode(v); err != nil {
		os.Exit(1)
	}
	if v.Verdict != "PROVED" {
		os.Exit(2)
	}
}
