package placement

import (
	"fmt"
	"sort"
)

func ChooseNode(spec ServiceSpec, nodes []Node, state State) (Decision, error) {
	candidates := append([]Node(nil), nodes...)
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].ID == state.ExistingNodeID {
			return true
		}
		if candidates[j].ID == state.ExistingNodeID {
			return false
		}
		if candidates[i].Name == candidates[j].Name {
			return candidates[i].ID < candidates[j].ID
		}
		return candidates[i].Name < candidates[j].Name
	})
	rejections := []string{}
	for _, node := range candidates {
		if reason := rejectReason(spec, node, state); reason != "" {
			rejections = append(rejections, node.ID+": "+reason)
			continue
		}
		return Decision{Node: node, Reasons: []string{"eligible"}}, nil
	}
	return Decision{}, fmt.Errorf("no eligible node: %v", rejections)
}

func rejectReason(spec ServiceSpec, node Node, state State) string {
	if !node.Reachable {
		return "node is unreachable"
	}
	if !node.OSSupported {
		return "node OS is unsupported"
	}
	if node.Maintenance {
		return "node is in maintenance"
	}
	if !node.Capabilities.Docker || !node.Capabilities.Compose || !node.Capabilities.Include {
		return "Docker, Compose, or Compose include is unavailable"
	}
	for key, want := range spec.Selector {
		if node.Traits[key] != want {
			return "selector mismatch"
		}
	}
	for _, dep := range spec.Dependencies {
		if dep.NodeID != "" && dep.NodeID != node.ID {
			return "dependency " + dep.ServiceID + " is placed on " + dep.NodeID
		}
	}
	for _, want := range spec.PortClaims {
		for _, claim := range state.ManagedPortClaims {
			if claim.NodeID == node.ID && claim.Port == want && claim.ServiceID != spec.ID {
				return "managed port claim conflict"
			}
		}
	}
	return ""
}
