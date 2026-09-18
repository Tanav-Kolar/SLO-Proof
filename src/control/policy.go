package control

// RecoveryAssessment is presentation and operator guidance, never an executor
// command. Contract compliance alone cannot choose the least harmful routing.
type RecoveryAssessment struct {
	IncidentStatus string `json:"incident_status"`
	PromotionGate  string `json:"promotion_gate"`
	RoutingAction  string `json:"routing_action"`
	Explanation    string `json:"explanation"`
	Scope          string `json:"scope"`
}

func Assess(v Verdict) RecoveryAssessment {
	a := RecoveryAssessment{
		IncidentStatus: "OPEN", PromotionGate: "BLOCKED",
		RoutingAction: "NO_AUTOMATIC_CHANGE", Scope: "OBSERVED_TRIAL_ONLY",
	}
	switch v.Verdict {
	case "PROVED":
		a.PromotionGate = "ELIGIBLE_FOR_OPERATOR_REVIEW"
		a.Explanation = "The candidate meets the frozen contract in the observed trial. An operator must decide whether to expand traffic; full-load recovery needs separate measurements."
	case "FAILED":
		a.Explanation = "The candidate does not meet the recovery contract. It may still improve on the incident version. Routing remains unchanged; an operator must choose the next mitigation. This verdict does not order a return to the previous version."
	default:
		a.Explanation = "The evidence cannot establish recovery. Routing remains unchanged and promotion is blocked pending adequate evidence and operator review."
	}
	return a
}
