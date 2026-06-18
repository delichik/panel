package orchestrator

import (
	"strconv"
	"strings"

	panelerr "panel/internal/platform/errors"
)

// Evaluate parses a trait selector expression and evaluates it against server traits.
// Syntax supported: key == "val" && sys.cores >= 4 || custom.env != "dev"
// Parentheses are not supported to keep it lightweight and secure.
// && has higher precedence than ||.
func Evaluate(selector string, traits map[string]string) (bool, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return true, nil // Empty selector matches everything
	}

	// 1. Split by || (OR has lower precedence, so it defines outer logical groups)
	orParts := splitLogicalOperator(selector, "||")
	for _, orPart := range orParts {
		orPart = strings.TrimSpace(orPart)
		if orPart == "" {
			return false, panelerr.Validation("invalid_selector", "Empty clause in selector around ||")
		}

		// 2. Split by && (AND has higher precedence)
		andParts := splitLogicalOperator(orPart, "&&")
		andMatch := true

		for _, andPart := range andParts {
			andPart = strings.TrimSpace(andPart)
			if andPart == "" {
				return false, panelerr.Validation("invalid_selector", "Empty clause in selector around &&")
			}

			matched, err := evaluateLeaf(andPart, traits)
			if err != nil {
				return false, err
			}
			if !matched {
				andMatch = false
				break
			}
		}

		// If all parts in this AND group match, the entire OR clause is true
		if andMatch {
			return true, nil
		}
	}

	return false, nil
}

// splitLogicalOperator splits a string by operator (&& or ||) but ignores inside quotes.
func splitLogicalOperator(s, op string) []string {
	var parts []string
	inQuotes := false
	quoteChar := rune(0)
	lastIdx := 0

	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if (r == '"' || r == '\'') && (i == 0 || runes[i-1] != '\\') {
			if inQuotes && quoteChar == r {
				inQuotes = false
			} else if !inQuotes {
				inQuotes = true
				quoteChar = r
			}
		}

		if !inQuotes && i+len(op) <= len(runes) && string(runes[i:i+len(op)]) == op {
			parts = append(parts, string(runes[lastIdx:i]))
			lastIdx = i + len(op)
			i += len(op) - 1 // skip operator
		}
	}
	parts = append(parts, string(runes[lastIdx:]))
	return parts
}

// evaluateLeaf evaluates a single comparison clause like `sys.cpu_cores >= 4`
func evaluateLeaf(clause string, traits map[string]string) (bool, error) {
	clause = strings.TrimSpace(clause)
	operators := []string{"==", "!=", ">=", "<=", ">", "<"}
	var op string
	var opIdx = -1

	// Search for the comparison operator, priority given to two-character operators
	for _, o := range operators {
		idx := strings.Index(clause, o)
		if idx != -1 {
			// Ensure it is not inside quotes
			if !isInsideQuotes(clause, idx) {
				op = o
				opIdx = idx
				break
			}
		}
	}

	if opIdx == -1 {
		return false, panelerr.Validation("invalid_selector", "Comparison operator missing in clause: "+clause)
	}

	left := strings.TrimSpace(clause[:opIdx])
	right := strings.TrimSpace(clause[opIdx+len(op):])

	if left == "" || right == "" {
		return false, panelerr.Validation("invalid_selector", "Malformed comparison clause: "+clause)
	}

	// Clean quotes from the right-hand value if present
	rightVal := right
	if (strings.HasPrefix(right, "\"") && strings.HasSuffix(right, "\"")) ||
		(strings.HasPrefix(right, "'") && strings.HasSuffix(right, "'")) {
		rightVal = right[1 : len(right)-1]
	}

	// Fetch target trait value
	leftVal, exists := traits[left]
	if !exists {
		// If custom trait is missing, we treat it as empty string
		leftVal = ""
	}

	// Comparison evaluation
	switch op {
	case "==":
		return leftVal == rightVal, nil
	case "!=":
		return leftVal != rightVal, nil
	}

	// Numeric comparison fallback
	leftNum, errLeft := strconv.ParseFloat(leftVal, 64)
	rightNum, errRight := strconv.ParseFloat(rightVal, 64)

	if errLeft == nil && errRight == nil {
		// Perform numeric comparison
		switch op {
		case ">":
			return leftNum > rightNum, nil
		case "<":
			return leftNum < rightNum, nil
		case ">=":
			return leftNum >= rightNum, nil
		case "<=":
			return leftNum <= rightNum, nil
		}
	}

	// Fallback to lexicographical string comparison
	switch op {
	case ">":
		return leftVal > rightVal, nil
	case "<":
		return leftVal < rightVal, nil
	case ">=":
		return leftVal >= rightVal, nil
	case "<=":
		return leftVal <= rightVal, nil
	}

	return false, nil
}

func isInsideQuotes(s string, targetIdx int) bool {
	inQuotes := false
	quoteChar := rune(0)
	runes := []rune(s)
	for i := 0; i < targetIdx && i < len(runes); i++ {
		r := runes[i]
		if (r == '"' || r == '\'') && (i == 0 || runes[i-1] != '\\') {
			if inQuotes && quoteChar == r {
				inQuotes = false
			} else if !inQuotes {
				inQuotes = true
				quoteChar = r
			}
		}
	}
	return inQuotes
}
