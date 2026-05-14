package cli

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/gregmundy/llamactl/internal/config"
	"github.com/spf13/cobra"
)

// secretKeys is the set of yaml tag names whose values must never be printed.
var secretKeys = map[string]bool{
	"api_key":  true,
	"hf_token": true,
}

// allowedKeys returns a map from yaml tag name → reflect.StructField for every
// exported field in config.Config that has a yaml tag. This is the canonical
// allowlist for `config get` and `config set`; adding a field to Config
// automatically extends the command.
func allowedKeys() map[string]reflect.StructField {
	t := reflect.TypeOf(config.Config{})
	m := make(map[string]reflect.StructField, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		// yaml tags can be "name,omitempty" — take only the name part.
		name := strings.SplitN(tag, ",", 2)[0]
		if name != "" {
			m[name] = f
		}
	}
	return m
}

// formatValue renders a single config field value for display. Zero values
// (empty string, 0 int) are shown as "(unset)". Secret fields are shown as
// "********  (set; redacted)" when set.
func formatValue(key string, val reflect.Value) string {
	switch val.Kind() {
	case reflect.String:
		s := val.String()
		if s == "" {
			return "(unset)"
		}
		if secretKeys[key] {
			return "********  (set; redacted)"
		}
		return s
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n := val.Int()
		if n == 0 {
			return "(unset)"
		}
		if secretKeys[key] {
			return "********  (set; redacted)"
		}
		return strconv.FormatInt(n, 10)
	default:
		return fmt.Sprintf("%v", val.Interface())
	}
}

func newConfigCmd(d *Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect and update llamactl configuration",
	}
	cmd.AddCommand(newConfigGetCmd(d))
	cmd.AddCommand(newConfigSetCmd(d))
	cmd.AddCommand(newConfigListCmd(d))
	return cmd
}

func newConfigGetCmd(d *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Print the current value of a configuration key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigGet(d, args[0])
		},
		ValidArgsFunction: completeConfigKeys,
	}
}

func runConfigGet(d *Deps, key string) error {
	keys := allowedKeys()
	field, ok := keys[key]
	if !ok {
		return fmt.Errorf("%w: unknown config key %q", ErrUserError, key)
	}

	cfgVal := reflect.ValueOf(*d.Config)
	val := cfgVal.FieldByIndex(field.Index)
	fmt.Fprintln(d.Stdout, formatValue(key, val))
	return nil
}

func newConfigSetCmd(d *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Update a configuration key and persist to disk",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigSet(d, args[0], args[1])
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			// Only complete the first positional (the key); values are
			// user-supplied free-form (port numbers, secrets, paths).
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return completeConfigKeys(cmd, args, toComplete)
		},
	}
}

func runConfigSet(d *Deps, key, value string) error {
	keys := allowedKeys()
	field, ok := keys[key]
	if !ok {
		return fmt.Errorf("%w: unknown config key %q", ErrUserError, key)
	}

	// Work on a draft; only update d.Config on successful Save.
	draft := *d.Config

	draftVal := reflect.ValueOf(&draft).Elem()
	fv := draftVal.FieldByIndex(field.Index)

	switch fv.Kind() {
	case reflect.String:
		if key == "log_level" {
			valid := map[string]bool{"debug": true, "info": true, "warn": true, "error": true, "": true}
			if !valid[value] {
				return fmt.Errorf("%w: log_level must be one of debug|info|warn|error (or empty to clear), got %q", ErrUserError, value)
			}
		}
		fv.SetString(value)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%w: %s must be an integer, got %q", ErrUserError, key, value)
		}
		if key == "default_port" && (n < 0 || n > 65535) {
			return fmt.Errorf("%w: default_port must be between 0 and 65535, got %d", ErrUserError, n)
		}
		fv.SetInt(int64(n))

	default:
		return fmt.Errorf("%w: unsupported field type for key %q", ErrUserError, key)
	}

	if err := config.Save(d.ConfigPath, draft); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	// Persist the mutated draft back to the shared pointer.
	*d.Config = draft

	fmt.Fprintf(d.Stdout, "%s updated\n", key)
	return nil
}

func newConfigListCmd(d *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all configuration keys and their current values",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigList(d)
		},
	}
}

func runConfigList(d *Deps) error {
	keys := allowedKeys()

	// Sort keys for deterministic output.
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	cfgVal := reflect.ValueOf(*d.Config)
	for _, k := range sorted {
		field := keys[k]
		val := cfgVal.FieldByIndex(field.Index)
		fmt.Fprintf(d.Stdout, "%-20s %s\n", k, formatValue(k, val))
	}
	return nil
}
