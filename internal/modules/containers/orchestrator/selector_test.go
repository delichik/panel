package orchestrator

import (
	"testing"
)

func TestEvaluateTraits(t *testing.T) {
	traits := map[string]string{
		"sys.cpu_cores":       "8",
		"sys.memory_total_mb": "16384",
		"sys.disk_total_gb":   "256",
		"sys.os":              "debian-13",
		"custom.env":          "prod",
		"custom.role":         "web",
	}

	tests := []struct {
		name     string
		selector string
		expected bool
		wantErr  bool
	}{
		{
			name:     "Empty selector matches all",
			selector: "",
			expected: true,
		},
		{
			name:     "Simple numeric equality",
			selector: "sys.cpu_cores == 8",
			expected: true,
		},
		{
			name:     "Simple numeric comparison true",
			selector: "sys.memory_total_mb >= 4096",
			expected: true,
		},
		{
			name:     "Simple numeric comparison false",
			selector: "sys.disk_total_gb < 100",
			expected: false,
		},
		{
			name:     "String equality true",
			selector: "custom.env == 'prod'",
			expected: true,
		},
		{
			name:     "String inequality true",
			selector: "custom.role != 'db'",
			expected: true,
		},
		{
			name:     "Logical AND true",
			selector: "sys.cpu_cores >= 4 && custom.env == \"prod\"",
			expected: true,
		},
		{
			name:     "Logical AND false",
			selector: "sys.cpu_cores >= 4 && custom.env == \"dev\"",
			expected: false,
		},
		{
			name:     "Logical OR true",
			selector: "custom.role == \"db\" || custom.role == \"web\"",
			expected: true,
		},
		{
			name: "Operator precedence - true AND took higher precedence",
			// (cpu >= 16 && env == 'prod') || role == 'web'
			// (false) || true => true
			selector: "sys.cpu_cores >= 16 && custom.env == 'prod' || custom.role == 'web'",
			expected: true,
		},
		{
			name: "Operator precedence - false",
			// (cpu >= 4 && env == 'dev') || role == 'db'
			// (false) || false => false
			selector: "sys.cpu_cores >= 4 && custom.env == 'dev' || custom.role == 'db'",
			expected: false,
		},
		{
			name:     "Missing trait treated as empty string",
			selector: "custom.nonexistent == ''",
			expected: true,
		},
		{
			name:     "Operator inside string literal ignored",
			selector: "custom.role == 'operator && literal'",
			expected: false,
		},
		{
			name:     "Malformed comparison clause returns error",
			selector: "sys.cpu_cores == ",
			wantErr:  true,
		},
		{
			name:     "Missing operator returns error",
			selector: "sys.cpu_cores",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Evaluate(tt.selector, traits)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Evaluate() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if err == nil && got != tt.expected {
				t.Fatalf("Evaluate() got = %v, expected = %v", got, tt.expected)
			}
		})
	}
}
