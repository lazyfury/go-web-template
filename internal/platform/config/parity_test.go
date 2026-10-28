package config

import (
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

// exampleConfigPath is the shipped, fully documented configuration file. Tests
// resolve it from the package directory (same convention as
// TestRepoConfigSecretsAreRejected).
const exampleConfigPath = "../../../configs/config.example.yaml"

// TestEveryConfigKeyHasADefaultAndAnExample keeps the three sources of truth in
// sync: the Config struct, the viper defaults and configs/config.example.yaml.
//
// Both drift directions have already happened in this repository: the example
// file was missing the whole redis/retention sections, and .env.example points
// readers at that file as the "complete key list". Without a nail the only
// enforcement is remembering to update three places when adding a key.
func TestEveryConfigKeyHasADefaultAndAnExample(t *testing.T) {
	structKeys := configLeafKeys(t)
	defaultKeys := defaultKeys(t)
	exampleKeys := exampleKeys(t)

	for _, key := range structKeys {
		if !contains(defaultKeys, key) {
			t.Errorf("config key %q has no viper default: add it to setDefaults so environment overrides work", key)
		}
	}
	for _, key := range defaultKeys {
		if !contains(structKeys, key) {
			t.Errorf("default %q does not correspond to a Config field (stale key in setDefaults?)", key)
		}
	}
	for _, key := range defaultKeys {
		if !contains(exampleKeys, key) {
			t.Errorf("config key %q is missing from configs/config.example.yaml: users cannot discover it", key)
		}
	}
	for _, key := range exampleKeys {
		if !contains(defaultKeys, key) {
			t.Errorf("configs/config.example.yaml sets unknown key %q (typo, or missing setDefaults entry)", key)
		}
	}
}

// TestExampleConfigIsValid makes sure the documented example is also a
// runnable configuration: a key can be present and still be rejected by
// Validate (for example a wildcard origin with credentials enabled).
func TestExampleConfigIsValid(t *testing.T) {
	v := viper.New()
	v.SetConfigFile(exampleConfigPath)
	v.SetConfigType("yaml")
	if err := v.ReadInConfig(); err != nil {
		t.Fatalf("read %s: %v", exampleConfigPath, err)
	}

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		t.Fatalf("unmarshal %s: %v", exampleConfigPath, err)
	}
	cfg.normalize()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("example config does not pass Validate: %v", err)
	}
}

// configLeafKeys walks Config and returns every mapstructure path that holds a
// scalar or a slice (the keys viper registers).
func configLeafKeys(t *testing.T) []string {
	t.Helper()

	var keys []string
	var walk func(prefix string, typ reflect.Type)
	walk = func(prefix string, typ reflect.Type) {
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if !field.IsExported() {
				continue
			}
			tag := field.Tag.Get("mapstructure")
			if tag == "" || tag == "-" {
				t.Fatalf("%s.%s has no mapstructure tag: the config key would be invisible to viper", typ.Name(), field.Name)
			}
			name, _, _ := strings.Cut(tag, ",")
			path := name
			if prefix != "" {
				path = prefix + "." + name
			}

			fieldType := field.Type
			for fieldType.Kind() == reflect.Pointer {
				fieldType = fieldType.Elem()
			}
			if fieldType.Kind() == reflect.Struct && fieldType != reflect.TypeOf(time.Time{}) {
				walk(path, fieldType)
				continue
			}
			keys = append(keys, path)
		}
	}
	walk("", reflect.TypeOf(Config{}))

	sort.Strings(keys)
	return keys
}

// defaultKeys returns every key registered by setDefaults.
func defaultKeys(t *testing.T) []string {
	t.Helper()

	v := viper.New()
	setDefaults(v)
	keys := v.AllKeys()
	sort.Strings(keys)
	return keys
}

// exampleKeys returns every key present in the example YAML. Keys with no value
// (for example an empty list) are still reported by viper.
func exampleKeys(t *testing.T) []string {
	t.Helper()

	v := viper.New()
	v.SetConfigFile(exampleConfigPath)
	v.SetConfigType("yaml")
	if err := v.ReadInConfig(); err != nil {
		t.Fatalf("read %s: %v", exampleConfigPath, err)
	}
	keys := v.AllKeys()
	sort.Strings(keys)
	return keys
}

func contains(sorted []string, key string) bool {
	i := sort.SearchStrings(sorted, key)
	return i < len(sorted) && sorted[i] == key
}
