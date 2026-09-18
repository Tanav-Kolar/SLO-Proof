package main

import (
	"encoding/json"
	"flag"
	"fmt"
	c "github.com/someshwaranM/forge-tyr/src/control"
	"os"
	"time"
)

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func emit(v any) { e := json.NewEncoder(os.Stdout); e.SetIndent("", "  "); check(e.Encode(v)) }
func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: controller freeze|trial|advance|observe|record|policy|route")
		os.Exit(1)
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	run := fs.String("run-id", "", "run ID")
	config := fs.String("config-hash", "", "live v1 hash")
	input := fs.String("input", "", "verifier result JSON")
	previous := fs.String("from-run", "", "previous proved stage for adjacent rollout progression")
	check(fs.Parse(os.Args[2:]))
	e := c.Elastic{Endpoint: os.Getenv("ELASTIC_CLOUD_ENDPOINT"), Key: os.Getenv("ELASTIC_CONTROLLER_API_KEY")}
	if cmd == "route" {
		r, err := c.GetRoute()
		check(err)
		emit(r)
		return
	}
	check(c.ValidateID(*run))
	switch cmd {
	case "freeze":
		r, err := c.GetRoute()
		check(err)
		f, err := c.Freeze(e, *run, "contracts/checkout-slo.json", "elastic/queries", *config, r)
		check(err)
		check(c.Append(e, *run, "contract_frozen", f.Hash, map[string]any{"state": "CONTRACT_FROZEN"}, nil))
		emit(f)
	case "trial":
		f, err := c.ReadFrozen(e, *run)
		check(err)
		_, _, err = f.ValidateLocal("contracts/checkout-slo.json", "elastic/queries")
		check(err)
		r, err := c.Execute(e, f, 10, 90)
		check(err)
		emit(r)
	case "observe":
		f, err := c.ReadFrozen(e, *run)
		check(err)
		r, err := c.ReadReceipt(e, *run, false)
		check(err)
		r, err = c.Effective(e, f, r)
		check(err)
		check(c.WriteReceipt(e, r, true))
		emit(r)
	case "policy":
		id, v, err := c.ReadRecordedVerdict(e, *run)
		check(err)
		f, err := c.ReadFrozen(e, *run)
		check(err)
		d, err := c.RecordRolloutPolicy(e, f, id, v)
		check(err)
		emit(d)
	case "advance":
		check(c.ValidateID(*previous))
		_, v, err := c.ReadRecordedVerdict(e, *previous)
		check(err)
		prior, err := c.ReadFrozen(e, *previous)
		check(err)
		next, err := c.ReadFrozen(e, *run)
		check(err)
		_, _, err = next.ValidateLocal("contracts/checkout-slo.json", "elastic/queries")
		check(err)
		weight, err := c.ValidateAdvance(prior, v, next, time.Now())
		check(err)
		check(c.Append(e, *run, "rollout_stage_authorized", next.Hash, map[string]any{"previous_run_id": *previous, "next_candidate_weight": weight, "plan": c.RolloutWeights}, nil))
		r, err := c.Execute(e, next, weight, 100-weight)
		check(err)
		emit(r)
	case "record":
		b, err := os.ReadFile(*input)
		check(err)
		var v c.Verdict
		check(json.Unmarshal(b, &v))
		if v.RunID != *run {
			check(fmt.Errorf("run mismatch"))
		}
		f, err := c.ReadFrozen(e, *run)
		check(err)
		if v.Hash != f.Hash {
			check(fmt.Errorf("contract mismatch"))
		}
		switch v.Verdict {
		case "PROVED", "FAILED", "INCONCLUSIVE":
		default:
			check(fmt.Errorf("invalid verdict"))
		}
		// Derive guidance from the verdict; never trust supplied action text.
		v.Assessment = c.Assess(v)
		check(c.Append(e, *run, "verdict", v.Hash, v, map[string]any{"verdict": v.Verdict, "reason": v.Reason, "version": v.Version, "actor": "deterministic-verifier"}))
		emit(map[string]any{"recorded": v.Verdict, "run_id": *run})
	default:
		check(fmt.Errorf("unknown command %q", cmd))
	}
}
