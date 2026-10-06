package main

import (
	"strings"
	"testing"
)

func TestProfileFlagCompletion(t *testing.T) {
	env, _, _ := testEnv(t)
	writeRegistry(t, env, twoProfiles)

	// Test the completion function for the -p/--profile flag directly.
	completeFunc := completeProfileKeys(env)
	root := newRoot(env)

	completions, directive := completeFunc(root, []string{}, "")

	// Should return profile keys.
	expectedKeys := map[string]bool{"perso": true, "acmecorp": true}
	for _, key := range completions {
		if !expectedKeys[key] {
			t.Errorf("unexpected completion key: %q", key)
		}
		delete(expectedKeys, key)
	}
	for key := range expectedKeys {
		t.Errorf("missing completion key: %q", key)
	}

	// Should use NoFileComp directive.
	// The value should be cobra.ShellCompDirectiveNoFileComp (4).
	if directive != 4 {
		t.Errorf("unexpected directive: %v (expected 4)", directive)
	}
}

func TestConfigGetCompletion(t *testing.T) {
	env, _, _ := testEnv(t)
	writeRegistry(t, env, twoProfiles)

	// Test completion for `dot config get <key>`.
	root := newRoot(env)
	configCmd, _, _ := root.Find([]string{"config"})
	getCmd, _, _ := configCmd.Find([]string{"get"})

	// Invoke the completion function directly.
	if getCmd.ValidArgsFunction == nil {
		t.Fatal("get command has no ValidArgsFunction")
	}

	completions, _ := getCmd.ValidArgsFunction(getCmd, []string{}, "")
	expectedKeys := map[string]bool{
		"default":               true,
		"profiles.perso.repo":   true,
		"profiles.acmecorp.repo": true,
	}

	for _, key := range completions {
		if !expectedKeys[key] {
			t.Errorf("unexpected completion key: %q", key)
		}
		delete(expectedKeys, key)
	}

	for key := range expectedKeys {
		t.Errorf("missing completion key: %q", key)
	}
}

func TestConfigUnsetCompletion(t *testing.T) {
	env, _, _ := testEnv(t)
	writeRegistry(t, env, twoProfiles)

	// Test completion for `dot config unset <key>`.
	root := newRoot(env)
	configCmd, _, _ := root.Find([]string{"config"})
	unsetCmd, _, _ := configCmd.Find([]string{"unset"})

	// Invoke the completion function directly.
	if unsetCmd.ValidArgsFunction == nil {
		t.Fatal("unset command has no ValidArgsFunction")
	}

	completions, _ := unsetCmd.ValidArgsFunction(unsetCmd, []string{}, "")
	expectedKeys := map[string]bool{
		"default":               true,
		"profiles.perso.repo":   true,
		"profiles.acmecorp.repo": true,
	}

	for _, key := range completions {
		if !expectedKeys[key] {
			t.Errorf("unexpected completion key: %q", key)
		}
		delete(expectedKeys, key)
	}

	for key := range expectedKeys {
		t.Errorf("missing completion key: %q", key)
	}
}

func TestConfigCompletionWithEmptyRegistry(t *testing.T) {
	env, _, _ := testEnv(t)

	// Test completion with no profiles registered.
	root := newRoot(env)
	configCmd, _, _ := root.Find([]string{"config"})
	getCmd, _, _ := configCmd.Find([]string{"get"})

	completions, _ := getCmd.ValidArgsFunction(getCmd, []string{}, "")

	// Should still have "default" even with no profiles.
	hasDefault := false
	for _, key := range completions {
		if key == "default" {
			hasDefault = true
		}
		// Should not have any profiles.<key>.repo since no profiles exist.
		if strings.HasPrefix(key, "profiles.") {
			t.Errorf("unexpected profile completion with empty registry: %q", key)
		}
	}
	if !hasDefault {
		t.Error("missing 'default' completion key")
	}
}
