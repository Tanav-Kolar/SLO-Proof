package control

import "testing"

func TestVerdictsNeverOrderRoutingOrCloseIncident(t *testing.T) {
	for _, verdict := range []string{"PROVED", "FAILED", "INCONCLUSIVE", "", "unknown"} {
		t.Run(verdict, func(t *testing.T) {
			a := Assess(Verdict{Verdict: verdict})
			if a.RoutingAction != "NO_AUTOMATIC_CHANGE" || a.IncidentStatus != "OPEN" {
				t.Fatalf("a trial verdict made an operational decision: %+v", a)
			}
			if verdict == "PROVED" {
				if a.PromotionGate != "ELIGIBLE_FOR_OPERATOR_REVIEW" {
					t.Fatal(a)
				}
			} else if a.PromotionGate != "BLOCKED" {
				t.Fatal(a)
			}
		})
	}
}

func TestImprovementDoesNotOverrideContractFailure(t *testing.T) {
	// A successful but 500ms candidate is better than an 800ms/erroring
	// baseline. Neither a pass nor automatic reversal follows from that fact.
	v := Verdict{Verdict: "FAILED", Windows: []Window{{Success: 1, P95: 500, Verdict: "FAILED"}}}
	a := Assess(v)
	if a.PromotionGate != "BLOCKED" || a.RoutingAction != "NO_AUTOMATIC_CHANGE" {
		t.Fatal(a)
	}
}
