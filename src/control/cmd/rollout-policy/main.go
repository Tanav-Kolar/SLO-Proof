// Offline deterministic classification. This binary has no execution or write path.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	c "github.com/someshwaranM/forge-tyr/src/control"
	"os"
)

func main() {
	input := flag.String("input", "", "saved verifier output")
	contract := flag.String("contract", "contracts/checkout-slo.json", "unchanged acceptance contract")
	retryOnly := flag.Bool("retry-only", false, "classify a read-only evidence retry, including errors before contract retrieval")
	flag.Parse()
	fail := func(err error) {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	b, err := os.ReadFile(*input)
	fail(err)
	var v c.Verdict
	fail(json.Unmarshal(b, &v))
	if *retryOnly {
		fail(json.NewEncoder(os.Stdout).Encode(map[string]any{"evidence_retry": c.RetryEvidence(v)}))
		return
	}
	criteria, raw, err := c.LoadContract(*contract)
	fail(err)
	if v.Hash != c.Hash(raw) {
		fail(fmt.Errorf("verdict and contract hash differ"))
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	fail(enc.Encode(c.DecideRollout(v, criteria)))
}
