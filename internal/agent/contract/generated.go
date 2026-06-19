package contract

import (
	"reflect"
	"sort"
	"strings"
	"time"
)

var timeType = reflect.TypeOf(time.Time{})

func CurrentContract() Contract {
	endpoints := []Endpoint{
		{ID: "health", Method: "GET", Path: "/v1/health", Response: schemaOf[HealthResponse]()},
		{ID: "os-release", Method: "GET", Path: "/v1/system/os-release", Response: schemaOf[OSReleaseResponse]()},
		{ID: "system-traits", Method: "GET", Path: "/v1/system/traits", Response: schemaOf[SystemTraitsResponse]()},
		{ID: "metrics-snapshot", Method: "GET", Path: "/v1/metrics/snapshot", Query: map[string]string{"serverId": "string"}, Response: schemaOf[MetricsSnapshotResponse]()},
		{ID: "packages-list", Method: "GET", Path: "/v1/system/packages/updates", Response: schemaOf[PackageUpdatesResponse]()},
		{ID: "packages-upgrade", Method: "POST", Path: "/v1/system/packages/upgrade", Request: schemaOf[PackageUpgradeRequest](), Response: schemaOf[CommandResponse]()},
		{ID: "ufw-status", Method: "GET", Path: "/v1/ufw/status", Response: schemaOf[UFWStatusResponse]()},
		{ID: "ufw-install", Method: "POST", Path: "/v1/ufw/install", Request: schemaOf[UFWInstallRequest](), Response: schemaOf[UFWStatusResponse]()},
		{ID: "ufw-enable", Method: "POST", Path: "/v1/ufw/enable", Request: schemaOf[UFWEnableRequest](), Response: schemaOf[UFWStatusResponse]()},
		{ID: "ufw-allow", Method: "POST", Path: "/v1/ufw/rules", Request: schemaOf[UFWAllowRequest](), Response: schemaOf[UFWStatusResponse]()},
		{ID: "ufw-delete", Method: "POST", Path: "/v1/ufw/rules/delete", Request: schemaOf[UFWDeleteRequest](), Response: schemaOf[UFWStatusResponse]()},
		{ID: "system-restart", Method: "POST", Path: "/v1/system/restart", Response: schemaOf[okResponse]()},
		{ID: "docker-containers", Method: "GET", Path: "/v1/docker/containers", Response: schemaOf[DockerContainersResponse]()},
		{ID: "docker-container-logs", Method: "GET", Path: "/v1/docker/containers/{id}/logs", Query: map[string]string{"tail": "int"}, Response: schemaOf[DockerContainerLogsResponse]()},
		{ID: "docker-container-action", Method: "POST", Path: "/v1/docker/containers/{id}/{action}", Response: schemaOf[okResponse]()},
		{ID: "docker-container-delete", Method: "DELETE", Path: "/v1/docker/containers/{id}", Response: schemaOf[okResponse]()},
		{ID: "docker-images", Method: "GET", Path: "/v1/docker/images", Response: schemaOf[DockerImagesResponse]()},
		{ID: "docker-image-pull", Method: "POST", Path: "/v1/docker/images/pull", Request: schemaOf[DockerImagePullRequest](), Response: schemaOf[okResponse]()},
		{ID: "docker-image-delete", Method: "DELETE", Path: "/v1/docker/images/{id}", Response: schemaOf[okResponse]()},
		{ID: "docker-networks", Method: "GET", Path: "/v1/docker/networks", Response: schemaOf[DockerNetworksResponse]()},
		{ID: "docker-volumes", Method: "GET", Path: "/v1/docker/volumes", Response: schemaOf[DockerVolumesResponse]()},
		{ID: "docker-volume-delete", Method: "DELETE", Path: "/v1/docker/volumes/{name}", Response: schemaOf[okResponse]()},
		{ID: "runtime-write-files", Method: "POST", Path: "/v1/runtime/applications/files", Request: schemaOf[RuntimeWriteFilesRequest](), Response: schemaOf[okResponse]()},
		{ID: "runtime-create-container", Method: "POST", Path: "/v1/runtime/applications/containers/create", Request: schemaOf[RuntimeCreateContainerRequest](), Response: schemaOf[RuntimeCreateContainerResponse]()},
		{ID: "runtime-stop", Method: "POST", Path: "/v1/runtime/applications/stop", Request: schemaOf[RuntimeStopRequest](), Response: schemaOf[RuntimeInstanceResponse]()},
		{ID: "runtime-restart", Method: "POST", Path: "/v1/runtime/applications/restart", Request: schemaOf[RuntimeRestartRequest](), Response: schemaOf[RuntimeInstanceResponse]()},
		{ID: "runtime-status", Method: "GET", Path: "/v1/runtime/applications/{instanceId}/status", Query: map[string]string{"containerName": "string"}, Response: schemaOf[RuntimeStatusResponse]()},
		{ID: "runtime-logs", Method: "GET", Path: "/v1/runtime/applications/{instanceId}/logs", Query: map[string]string{"containerName": "string", "tail": "int"}, Response: schemaOf[RuntimeLogsResponse]()},
		{ID: "runtime-persistent-archive", Method: "GET", Path: "/v1/runtime/applications/{applicationId}/persistent/archive", Response: schemaOf[RuntimePersistentArchiveResponse]()},
		{ID: "runtime-persistent-restore", Method: "POST", Path: "/v1/runtime/applications/{applicationId}/persistent/restore", Request: schemaOf[RuntimePersistentRestoreRequest](), Response: schemaOf[RuntimePersistentRestoreResponse]()},
	}
	sort.Slice(endpoints, func(i, j int) bool { return endpoints[i].ID < endpoints[j].ID })
	return Contract{Endpoints: endpoints}
}

func MissingEndpoints(actual Contract) []string {
	return Missing(CurrentContract(), actual)
}

type okResponse struct {
	OK bool `json:"ok"`
}

func schemaOf[T any]() *Schema {
	var value T
	schema := schemaForType(reflect.TypeOf(value), false, map[reflect.Type]bool{})
	return &schema
}

func schemaForType(t reflect.Type, optional bool, seen map[reflect.Type]bool) Schema {
	if t == nil {
		return Schema{Type: "null", Optional: optional}
	}
	for t.Kind() == reflect.Pointer {
		optional = true
		t = t.Elem()
	}
	if t == timeType {
		return Schema{Type: "time", Optional: optional}
	}
	switch t.Kind() {
	case reflect.Bool:
		return Schema{Type: "bool", Optional: optional}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return Schema{Type: "int", Optional: optional}
	case reflect.Float32, reflect.Float64:
		return Schema{Type: "float", Optional: optional}
	case reflect.String:
		return Schema{Type: "string", Optional: optional}
	case reflect.Slice, reflect.Array:
		item := schemaForType(t.Elem(), false, seen)
		return Schema{Type: "array", Optional: optional, Items: &item}
	case reflect.Map:
		additional := schemaForType(t.Elem(), false, seen)
		return Schema{Type: "object", Optional: optional, Additional: &additional}
	case reflect.Struct:
		if seen[t] {
			return Schema{Type: "object", Optional: optional}
		}
		seen[t] = true
		fields := map[string]Schema{}
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if field.PkgPath != "" {
				continue
			}
			name, fieldOptional, skip := jsonField(field)
			if skip {
				continue
			}
			fieldSchema := schemaForType(field.Type, fieldOptional, seen)
			if field.Anonymous && name == "" && fieldSchema.Type == "object" {
				for key, value := range fieldSchema.Fields {
					fields[key] = value
				}
				continue
			}
			fields[name] = fieldSchema
		}
		delete(seen, t)
		return Schema{Type: "object", Optional: optional, Fields: fields}
	default:
		return Schema{Type: t.Kind().String(), Optional: optional}
	}
}

func jsonField(field reflect.StructField) (string, bool, bool) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", false, true
	}
	parts := strings.Split(tag, ",")
	name := strings.TrimSpace(parts[0])
	if name == "" {
		name = field.Name
	}
	optional := false
	for _, part := range parts[1:] {
		if strings.TrimSpace(part) == "omitempty" {
			optional = true
		}
	}
	return name, optional, false
}
