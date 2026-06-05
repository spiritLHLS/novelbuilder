package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

type modelPolicyDocument struct {
	Scope             string   `json:"scope"`
	AllowedProfileIDs []string `json:"allowed_profile_ids"`
	ProfileIDs        []string `json:"profile_ids"`
	AllowDefault      *bool    `json:"allow_default"`
}

// ModelPolicyAllowsProfile evaluates a user's model policy against an LLM
// profile id. A nil or empty profile id means the global default profile.
func ModelPolicyAllowsProfile(raw string, profileID *string) bool {
	var policy modelPolicyDocument
	hasExplicitProfileList := false
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &policy)
		var doc map[string]json.RawMessage
		if json.Unmarshal([]byte(raw), &doc) == nil {
			_, hasAllowed := doc["allowed_profile_ids"]
			_, hasProfiles := doc["profile_ids"]
			hasExplicitProfileList = hasAllowed || hasProfiles
		}
	}
	scope := strings.ToLower(strings.TrimSpace(policy.Scope))
	if scope == "all" {
		return true
	}
	if scope == "none" {
		return false
	}

	if profileID == nil || strings.TrimSpace(*profileID) == "" {
		return policy.AllowDefault == nil || *policy.AllowDefault
	}
	if !hasExplicitProfileList {
		return true
	}

	allowed := append([]string{}, policy.AllowedProfileIDs...)
	allowed = append(allowed, policy.ProfileIDs...)
	for _, id := range allowed {
		if strings.TrimSpace(id) == strings.TrimSpace(*profileID) {
			return true
		}
	}
	return false
}

func (s *AgentRoutingService) profileAllowedForProject(ctx context.Context, projectID string, profileID *string) bool {
	if s == nil || s.db == nil || strings.TrimSpace(projectID) == "" {
		return true
	}
	var policy sql.NullString
	err := s.db.QueryRow(ctx,
		`SELECT u.model_policy
		   FROM projects p
		   LEFT JOIN users u ON u.id = p.owner_id
		  WHERE p.id = $1`,
		projectID).Scan(&policy)
	if err != nil {
		return true
	}
	if !policy.Valid {
		return true
	}
	return ModelPolicyAllowsProfile(policy.String, profileID)
}
