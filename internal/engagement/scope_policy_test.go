package engagement

import "testing"

func TestCheckTargetScopeExplicitExclusionOverridesCIDR(t *testing.T) {
	scope := []string{"10.20.0.0/16", "!10.20.10.5", "!https://10.20.10.6/admin"}
	if decision := CheckTargetScope(scope, "http://10.20.10.4/"); !decision.Allowed {
		t.Fatalf("allowed CIDR host rejected: %#v", decision)
	}
	if decision := CheckTargetScope(scope, "http://10.20.10.5/"); decision.Allowed || decision.Matched != "!10.20.10.5" {
		t.Fatalf("excluded host accepted: %#v", decision)
	}
	if decision := CheckTargetScope(scope, "https://10.20.10.6/admin/users"); decision.Allowed || decision.Matched != "!https://10.20.10.6/admin" {
		t.Fatalf("excluded URL prefix accepted: %#v", decision)
	}
	if decision := CheckTargetScope(scope, "https://10.20.10.6/public"); !decision.Allowed {
		t.Fatalf("non-excluded path inside CIDR rejected: %#v", decision)
	}
}

func TestCheckTargetScopeRelativePathUsesPrimaryTargetAndRestrictions(t *testing.T) {
	scope := []string{"https://primary.example/admin", "https://second.example/", "!https://primary.example/admin/private"}
	if decision := CheckTargetScope(scope, "/admin/users"); !decision.Allowed || decision.Matched != scope[0] {
		t.Fatalf("primary relative path rejected: %#v", decision)
	}
	if decision := CheckTargetScope(scope, "/admin/private/keys"); decision.Allowed {
		t.Fatalf("relative exclusion bypassed: %#v", decision)
	}
	if decision := CheckTargetScope(scope, "/public"); decision.Allowed {
		t.Fatalf("relative path broadened primary URL prefix: %#v", decision)
	}
	if decision := CheckTargetScope([]string{"10.20.0.0/16"}, "/admin"); decision.Allowed {
		t.Fatalf("relative path inherited ambiguous CIDR: %#v", decision)
	}
}
