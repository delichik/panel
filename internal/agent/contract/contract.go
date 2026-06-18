// Package contract defines the stable, implementation-independent Agent HTTP
// contract model and compatibility rules shared by the Panel and Agent sides.
package contract

type Contract struct {
	Endpoints []Endpoint `json:"endpoints"`
}

type Endpoint struct {
	ID       string            `json:"id"`
	Method   string            `json:"method"`
	Path     string            `json:"path"`
	Query    map[string]string `json:"query,omitempty"`
	Request  *Schema           `json:"request,omitempty"`
	Response *Schema           `json:"response,omitempty"`
}

type Schema struct {
	Type       string            `json:"type"`
	Optional   bool              `json:"optional,omitempty"`
	Fields     map[string]Schema `json:"fields,omitempty"`
	Items      *Schema           `json:"items,omitempty"`
	Additional *Schema           `json:"additional,omitempty"`
}

func Missing(required, actual Contract) []string {
	available := map[string]Endpoint{}
	for _, endpoint := range actual.Endpoints {
		available[endpoint.ID] = endpoint
	}
	missing := []string{}
	for _, requiredEndpoint := range required.Endpoints {
		got, ok := available[requiredEndpoint.ID]
		if !ok || !endpointCompatible(requiredEndpoint, got) {
			missing = append(missing, requiredEndpoint.ID)
		}
	}
	return missing
}

func endpointCompatible(required, actual Endpoint) bool {
	if required.Method != actual.Method || required.Path != actual.Path {
		return false
	}
	if !queryCompatible(required.Query, actual.Query) {
		return false
	}
	return schemaCompatible(required.Request, actual.Request) &&
		schemaCompatible(required.Response, actual.Response)
}

func queryCompatible(required, actual map[string]string) bool {
	for key, want := range required {
		if actual[key] != want {
			return false
		}
	}
	return true
}

func schemaCompatible(required, actual *Schema) bool {
	if required == nil {
		return true
	}
	if actual == nil {
		return false
	}
	return schemaValueCompatible(*required, *actual)
}

func schemaValueCompatible(required, actual Schema) bool {
	if required.Type != actual.Type {
		return false
	}
	if !required.Optional && actual.Optional {
		return false
	}
	switch required.Type {
	case "object":
		for key, requiredField := range required.Fields {
			actualField, ok := actual.Fields[key]
			if !ok {
				if requiredField.Optional {
					continue
				}
				return false
			}
			if !schemaValueCompatible(requiredField, actualField) {
				return false
			}
		}
		if required.Additional != nil {
			return actual.Additional != nil &&
				schemaValueCompatible(*required.Additional, *actual.Additional)
		}
	case "array":
		if required.Items == nil {
			return true
		}
		return actual.Items != nil &&
			schemaValueCompatible(*required.Items, *actual.Items)
	}
	return true
}
