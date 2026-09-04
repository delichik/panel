package orm

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// columnTarget describes how one result column maps into a struct field.
type columnTarget struct {
	field *fieldInfo
	raw   *any // nil when the field is scanned directly (kindScanner)
}

// scanTargets builds scan destinations for one struct value. Non-scanner
// fields receive a *any which applyRaw converts afterwards.
func scanTargets(v reflect.Value, meta *modelInfo, cols []string) ([]any, []columnTarget, error) {
	targets := make([]any, len(cols))
	plan := make([]columnTarget, len(cols))
	for i, c := range cols {
		f := meta.byColumn[c]
		plan[i].field = f
		if f == nil {
			targets[i] = new(any)
			continue
		}
		if f.kind == kindScanner {
			targets[i] = v.FieldByIndex(f.index).Addr().Interface()
			continue
		}
		raw := new(any)
		targets[i] = raw
		plan[i].raw = raw
	}
	return targets, plan, nil
}

// applyRaw converts a raw driver value into the struct field.
func applyRaw(fv reflect.Value, f *fieldInfo, raw any) error {
	if raw == nil {
		// Leave zero values and nil pointers untouched.
		return nil
	}
	switch f.kind {
	case kindString:
		s, err := asString(raw)
		if err != nil {
			return err
		}
		return setString(fv, s)
	case kindInt:
		n, err := asInt64(raw)
		if err != nil {
			return err
		}
		return setInt(fv, n)
	case kindUint:
		n, err := asInt64(raw)
		if err != nil {
			return err
		}
		return setUint(fv, n)
	case kindFloat:
		x, err := asFloat64(raw)
		if err != nil {
			return err
		}
		return setFloat(fv, x)
	case kindBool:
		b, err := asBool(raw)
		if err != nil {
			return err
		}
		return setBool(fv, b)
	case kindTime:
		if blankStringValue(raw) {
			// 存量 SQLite 行可能用空串表示“未设置”的时间（例如 jobs 的
			// next_run_at/lease_expires_at 曾被写成 ''）。按未设置处理：
			// 指针字段保持 nil，避免 orm: cannot parse time "" 错误。
			return nil
		}
		tm, err := asTime(raw)
		if err != nil {
			return err
		}
		return setTime(fv, tm)
	case kindBytes:
		b, err := asBytes(raw)
		if err != nil {
			return err
		}
		return setBytes(fv, b)
	case kindJSON:
		b, err := asBytes(raw)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(b, fv.Addr().Interface()); err != nil {
			return fmt.Errorf("orm: decode json column: %w", err)
		}
		return nil
	case kindAny:
		fv.Set(reflect.ValueOf(raw))
		return nil
	default:
		return fmt.Errorf("orm: cannot scan kind %d", f.kind)
	}
}

func setString(fv reflect.Value, s string) error {
	if fv.Kind() == reflect.Pointer {
		p := reflect.New(fv.Type().Elem())
		p.Elem().SetString(s)
		fv.Set(p)
		return nil
	}
	fv.SetString(s)
	return nil
}

func setInt(fv reflect.Value, n int64) error {
	if fv.Kind() == reflect.Pointer {
		p := reflect.New(fv.Type().Elem())
		p.Elem().SetInt(n)
		fv.Set(p)
		return nil
	}
	fv.SetInt(n)
	return nil
}

func setUint(fv reflect.Value, n int64) error {
	if fv.Kind() == reflect.Pointer {
		p := reflect.New(fv.Type().Elem())
		p.Elem().SetUint(uint64(n))
		fv.Set(p)
		return nil
	}
	fv.SetUint(uint64(n))
	return nil
}

func setFloat(fv reflect.Value, x float64) error {
	if fv.Kind() == reflect.Pointer {
		p := reflect.New(fv.Type().Elem())
		p.Elem().SetFloat(x)
		fv.Set(p)
		return nil
	}
	fv.SetFloat(x)
	return nil
}

func setBool(fv reflect.Value, b bool) error {
	if fv.Kind() == reflect.Pointer {
		p := reflect.New(fv.Type().Elem())
		p.Elem().SetBool(b)
		fv.Set(p)
		return nil
	}
	fv.SetBool(b)
	return nil
}

func setTime(fv reflect.Value, tm time.Time) error {
	if fv.Kind() == reflect.Pointer {
		p := reflect.New(fv.Type().Elem())
		p.Elem().Set(reflect.ValueOf(tm).Convert(fv.Type().Elem()))
		fv.Set(p)
		return nil
	}
	fv.Set(reflect.ValueOf(tm).Convert(fv.Type()))
	return nil
}

func setBytes(fv reflect.Value, b []byte) error {
	if fv.Kind() == reflect.Pointer {
		p := reflect.New(fv.Type().Elem())
		p.Elem().SetBytes(b)
		fv.Set(p)
		return nil
	}
	fv.SetBytes(b)
	return nil
}

func asString(raw any) (string, error) {
	switch v := raw.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64), nil
	case time.Time:
		return v.UTC().Format(time.RFC3339Nano), nil
	case bool:
		if v {
			return "1", nil
		}
		return "0", nil
	default:
		return "", fmt.Errorf("orm: cannot convert %T to string", raw)
	}
}

func asInt64(raw any) (int64, error) {
	switch v := raw.(type) {
	case int64:
		return v, nil
	case int:
		return int64(v), nil
	case float64:
		return int64(v), nil
	case bool:
		if v {
			return 1, nil
		}
		return 0, nil
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("orm: cannot parse int %q", v)
		}
		return n, nil
	case []byte:
		return asInt64(string(v))
	case time.Time:
		return v.Unix(), nil
	default:
		return 0, fmt.Errorf("orm: cannot convert %T to int", raw)
	}
}

func asFloat64(raw any) (float64, error) {
	switch v := raw.(type) {
	case float64:
		return v, nil
	case int64:
		return float64(v), nil
	case int:
		return float64(v), nil
	case string:
		x, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, fmt.Errorf("orm: cannot parse float %q", v)
		}
		return x, nil
	case []byte:
		return asFloat64(string(v))
	default:
		return 0, fmt.Errorf("orm: cannot convert %T to float", raw)
	}
}

func asBool(raw any) (bool, error) {
	switch v := raw.(type) {
	case bool:
		return v, nil
	case int64:
		return v != 0, nil
	case int:
		return v != 0, nil
	case string:
		switch strings.TrimSpace(v) {
		case "1", "true", "TRUE", "True":
			return true, nil
		case "0", "false", "FALSE", "False":
			return false, nil
		}
		return false, fmt.Errorf("orm: cannot parse bool %q", v)
	case []byte:
		return asBool(string(v))
	default:
		return false, fmt.Errorf("orm: cannot convert %T to bool", raw)
	}
}

func asTime(raw any) (time.Time, error) {
	switch v := raw.(type) {
	case time.Time:
		return v, nil
	case int64:
		return time.Unix(v, 0), nil
	case float64:
		return time.Unix(int64(v), 0), nil
	case string:
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return t, nil
		}
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t, nil
		}
		return time.Time{}, fmt.Errorf("orm: cannot parse time %q", v)
	case []byte:
		return asTime(string(v))
	default:
		return time.Time{}, fmt.Errorf("orm: cannot convert %T to time", raw)
	}
}

func asBytes(raw any) ([]byte, error) {
	switch v := raw.(type) {
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	default:
		return nil, fmt.Errorf("orm: cannot convert %T to bytes", raw)
	}
}
