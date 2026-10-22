package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NamTranQuoc/shlink-go/internal/phpx"
)

// LoadEnvVarsFromParams ports what config/container.php does first: env vars are promoted from the installer's config
// files (config/params/*.php), or from config/test/shlink_test_env.php in the test env.
func LoadEnvVarsFromParams() error {
	pattern := "config/params/*.php"
	if IsTestEnv() {
		pattern = "config/test/shlink_test_env.php"
	}
	return LoadEnvVarsFromConfig(pattern, IsValid)
}

// LoadEnvVarsFromConfig ports shlink-config's loadEnvVarsFromConfig(): the config the files matching the glob return
// is merged, and every allowed key whose env var is not defined yet becomes one.
func LoadEnvVarsFromConfig(pattern string, allowed func(string) bool) error {
	config, err := LoadConfigFromGlob(pattern)
	if err != nil {
		return err
	}
	for _, it := range config.Items() {
		key, ok := it.Key.(string)
		if !ok || !allowed(key) {
			continue
		}
		putNotYetDefinedEnv(key, it.Value)
	}
	return nil
}

// LoadConfigFromGlob ports shlink-config's loadConfigFromGlob(): the arrays returned by the matching PHP files, in
// alphabetical order, merged with ArrayUtils::merge().
func LoadConfigFromGlob(pattern string) (*phpx.Array, error) {
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	config := phpx.NewArray()
	for _, file := range files {
		info, err := os.Stat(file)
		if !strings.HasSuffix(file, ".php") || err != nil || info.IsDir() {
			continue
		}
		value, err := IncludeConfigFile(file)
		if err != nil {
			return nil, err
		}
		config = phpx.Merge(config, value)
	}
	return config, nil
}

// IncludeConfigFile evaluates a PHP config file returning an array, like `include $file` does for the files
// shlink-installer writes.
func IncludeConfigFile(file string) (*phpx.Array, error) {
	src, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	value, err := phpx.ParseReturnFile(src)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	array, ok := value.(*phpx.Array)
	if !ok {
		return nil, fmt.Errorf("%s: the file does not return an array", file)
	}
	return array, nil
}

// putNotYetDefinedEnv ports shlink-config's putNotYetDefinedEnv().
func putNotYetDefinedEnv(key string, value any) {
	if !Env(key, Null).IsNull() {
		return
	}
	formatted, ok := formatEnvVarValueOrNull(value)
	if !ok {
		return
	}
	_ = os.Setenv(key, formatted)
}

// formatEnvVarValueOrNull ports shlink-config's formatEnvVarValueOrNull(); false means null.
func formatEnvVarValueOrNull(value any) (string, bool) {
	switch v := value.(type) {
	case nil:
		return "", false
	case *phpx.Array:
		return phpx.Implode(",", v), true
	case bool:
		if v {
			return "true", true
		}
		return "false", true
	}
	return phpx.ToString(value), true
}
