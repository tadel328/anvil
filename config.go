package anvil

import (
	"encoding"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type envField struct {
	owner    string
	key      string
	help     string
	def      string
	hasDef   bool
	required bool
	secret   bool
	value    reflect.Value
}

var (
	textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()
	durationType        = reflect.TypeFor[time.Duration]()
)

// Tags of a field:
//
//	env:"NAME"            the variable; options: required, secret
//	default:"value"       used when the variable is unset or empty
//	help:"text"           printed by App.PrintEnv
//	prefix:"REPLICA_"     on a nested struct, put before the names inside it
func LoadConfig(cfg any, prefix string, lookup func(string) (string, bool)) error {
	fields, err := collectFields("config", cfg, prefix)
	if err != nil {
		return err
	}

	return loadFields(fields, lookup)
}

func collectFields(owner string, cfg any, prefix string) ([]envField, error) {
	v := reflect.ValueOf(cfg)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return nil, fmt.Errorf("%s: config must be a non-nil pointer to a struct, got %T", owner, cfg)
	}

	var fields []envField
	if err := walkStruct(owner, v.Elem(), prefix, &fields); err != nil {
		return nil, err
	}

	return fields, nil
}

func walkStruct(owner string, v reflect.Value, prefix string, out *[]envField) error {
	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}

		tag, tagged := sf.Tag.Lookup("env")
		if !tagged {
			if sf.Type.Kind() == reflect.Struct && !reflect.PointerTo(sf.Type).Implements(textUnmarshalerType) {
				if err := walkStruct(owner, v.Field(i), prefix+sf.Tag.Get("prefix"), out); err != nil {
					return err
				}
			}
			continue
		}

		if tag == "-" {
			continue
		}

		name, opts, _ := strings.Cut(tag, ",")
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("%s: field %s has an env tag without a name", owner, sf.Name)
		}

		f := envField{
			owner: owner,
			key:   prefix + name,
			help:  sf.Tag.Get("help"),
			value: v.Field(i),
		}
		f.def, f.hasDef = sf.Tag.Lookup("default")

		for _, opt := range strings.Split(opts, ",") {
			switch strings.TrimSpace(opt) {
			case "":
			case "required":
				f.required = true
			case "secret":
				f.secret = true
			default:
				return fmt.Errorf("%s: field %s: unknown env option %q", owner, sf.Name, opt)
			}
		}

		if !supported(sf.Type) {
			return fmt.Errorf("%s: field %s: type %s cannot be read from the environment", owner, sf.Name, sf.Type)
		}

		*out = append(*out, f)
	}

	return nil
}

func supported(t reflect.Type) bool {
	if reflect.PointerTo(t).Implements(textUnmarshalerType) {
		return true
	}

	switch t.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	case reflect.Slice, reflect.Pointer:
		return supported(t.Elem())
	default:
		return false
	}
}

func loadFields(fields []envField, lookup func(string) (string, bool)) error {
	var errs []error

	for _, f := range fields {
		raw, ok := lookup(f.key)
		if !ok || raw == "" {
			switch {
			case f.hasDef:
				raw = f.def
			case f.required:
				errs = append(errs, fmt.Errorf("%s: %s is required", f.owner, f.key))
				continue
			default:
				continue
			}
		}

		if err := setValue(f.value, raw); err != nil {
			if f.secret {
				err = errors.New("invalid value")
			}
			errs = append(errs, fmt.Errorf("%s: %s: %w", f.owner, f.key, err))
		}
	}

	return errors.Join(errs...)
}

func setValue(v reflect.Value, raw string) error {
	if v.CanAddr() {
		if u, ok := v.Addr().Interface().(encoding.TextUnmarshaler); ok {
			return u.UnmarshalText([]byte(raw))
		}
	}

	if v.Type() == durationType {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return err
		}
		v.SetInt(int64(d))
		return nil
	}

	switch v.Kind() {
	case reflect.String:
		v.SetString(raw)

	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		v.SetBool(b)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 0, v.Type().Bits())
		if err != nil {
			return err
		}
		v.SetInt(n)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 0, v.Type().Bits())
		if err != nil {
			return err
		}
		v.SetUint(n)

	case reflect.Float32, reflect.Float64:
		n, err := strconv.ParseFloat(raw, v.Type().Bits())
		if err != nil {
			return err
		}
		v.SetFloat(n)

	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			v.SetBytes([]byte(raw))
			return nil
		}

		parts := splitList(raw)
		s := reflect.MakeSlice(v.Type(), len(parts), len(parts))
		for i, part := range parts {
			if err := setValue(s.Index(i), part); err != nil {
				return fmt.Errorf("item %d: %w", i, err)
			}
		}
		v.Set(s)

	case reflect.Pointer:
		p := reflect.New(v.Type().Elem())
		if err := setValue(p.Elem(), raw); err != nil {
			return err
		}
		v.Set(p)

	default:
		return fmt.Errorf("unsupported type %s", v.Type())
	}

	return nil
}

func splitList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}

	return parts
}

func (a *App) PrintEnv(w io.Writer) error {
	var b strings.Builder

	fmt.Fprintf(&b, "# environment of %s\n", a.name)

	owner := ""
	for _, f := range a.fields {
		if f.owner != owner {
			owner = f.owner
			fmt.Fprintf(&b, "\n# --- %s\n", owner)
		}

		value, set := a.lookup(f.key)
		if !set || value == "" {
			value, set = f.def, false
		}

		var notes []string
		if f.required {
			notes = append(notes, "required")
		}
		if f.hasDef {
			notes = append(notes, "default: "+f.def)
		}
		if f.secret && set {
			notes = append(notes, "secret, set")
			value = ""
		}

		comment := strings.Join(strings.Fields(f.help), " ")
		if len(notes) > 0 {
			comment = strings.TrimSpace(comment + " (" + strings.Join(notes, "; ") + ")")
		}
		if comment != "" {
			fmt.Fprintf(&b, "# %s\n", comment)
		}

		fmt.Fprintf(&b, "%s=%s\n", f.key, envQuote(value))
	}

	_, err := io.WriteString(w, b.String())
	return err
}
