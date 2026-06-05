package services

import "testing"

func TestModelPolicyAllowsProfile(t *testing.T) {
	profileA := "profile-a"
	profileB := "profile-b"

	tests := []struct {
		name      string
		policy    string
		profileID *string
		want      bool
	}{
		{name: "empty policy allows default", policy: `{}`, profileID: nil, want: true},
		{name: "empty policy allows explicit profile", policy: `{}`, profileID: &profileA, want: true},
		{name: "scope all allows explicit profile", policy: `{"scope":"all"}`, profileID: &profileA, want: true},
		{name: "scope none denies default", policy: `{"scope":"none"}`, profileID: nil, want: false},
		{name: "allowed profile ids allow matching explicit profile", policy: `{"allowed_profile_ids":["profile-a"]}`, profileID: &profileA, want: true},
		{name: "allowed profile ids deny nonmatching explicit profile", policy: `{"allowed_profile_ids":["profile-a"]}`, profileID: &profileB, want: false},
		{name: "empty allowed profile ids deny explicit profile", policy: `{"allowed_profile_ids":[]}`, profileID: &profileA, want: false},
		{name: "allow default false denies default", policy: `{"allow_default":false}`, profileID: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ModelPolicyAllowsProfile(tt.policy, tt.profileID); got != tt.want {
				t.Fatalf("ModelPolicyAllowsProfile() = %v, want %v", got, tt.want)
			}
		})
	}
}
