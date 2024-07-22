package rbac

import (
	"github.com/casbin/casbin/v2/model"
)

type NoopAdapter struct {
}

// LoadPolicy loads all policy rules from the storage.
func (a NoopAdapter) LoadPolicy(model model.Model) error {
	return nil
}

// SavePolicy saves all policy rules to the storage.
func (a NoopAdapter) SavePolicy(model model.Model) error {
	return nil
}

// AddPolicy adds a policy rule to the storage.
func (a NoopAdapter) AddPolicy(sec string, ptype string, rule []string) error {
	return nil

}

// AddPolicies removes policy rules from the storage.
func (a NoopAdapter) AddPolicies(sec string, ptype string, rules [][]string) error {
	return nil

}

// RemovePolicy removes a policy rule from the storage.
func (a NoopAdapter) RemovePolicy(sec string, ptype string, rule []string) error {
	return nil

}

// RemovePolicies removes policy rules from the storage.
func (a NoopAdapter) RemovePolicies(sec string, ptype string, rules [][]string) error {
	return nil

}

// UpdatePolicy removes a policy rule from the storage.
func (a NoopAdapter) UpdatePolicy(sec string, ptype string, oldRule, newPolicy []string) error {
	return nil

}

func (a NoopAdapter) UpdatePolicies(sec string, ptype string, oldRules, newRules [][]string) error {
	return nil

}

// RemoveFilteredPolicy removes policy rules that match the filter from the storage.
func (a NoopAdapter) RemoveFilteredPolicy(sec string, ptype string, fieldIndex int, fieldValues ...string) error {
	return nil

}
