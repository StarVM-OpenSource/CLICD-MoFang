package api

import (
	"strings"
	"testing"

	"clicd/internal/config"
)

func TestHashAPIKeyUsesSaltedArgon2idHash(t *testing.T) {
	raw := "clicd_sk_0123456789abcdef0123456789abcdef"

	h1, err := hashAPIKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := hashAPIKey(raw)
	if err != nil {
		t.Fatal(err)
	}

	if h1 == h2 {
		t.Fatal("expected salted hashes to differ")
	}
	if !strings.HasPrefix(h1, apiKeyHashPrefix+"$") || !strings.HasPrefix(h2, apiKeyHashPrefix+"$") {
		t.Fatalf("expected argon2id hashes, got %q and %q", h1, h2)
	}
	if !verifyAPIKeyHash(raw, h1) || !verifyAPIKeyHash(raw, h2) {
		t.Fatal("argon2id hashes did not verify")
	}
	if verifyAPIKeyHash(raw+"x", h1) {
		t.Fatal("argon2id hash verified wrong key")
	}
}

func TestValidateApiKeyAllowsArgon2idAndUpdatesLastUsed(t *testing.T) {
	raw := "clicd_sk_0123456789abcdef0123456789abcdef"
	hash, err := hashAPIKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	config.AppConfig = &config.ClicdConfig{
		ApiKeys: []config.ApiKeyConfig{{
			ID:      "key1",
			Name:    "test",
			KeyHash: hash,
		}},
	}

	if !validateApiKey(raw, "127.0.0.1") {
		t.Fatal("validateApiKey rejected valid argon2id key")
	}
	updateApiKeyLastUsed(raw)
	if config.AppConfig.ApiKeys[0].LastUsed == "" {
		t.Fatal("LastUsed was not updated")
	}
}

func TestValidateApiKeyMigratesLegacyHash(t *testing.T) {
	raw := "clicd_sk_0123456789abcdef0123456789abcdef"
	config.AppConfig = &config.ClicdConfig{
		ApiKeys: []config.ApiKeyConfig{{
			ID:      "legacy",
			Name:    "legacy",
			KeyHash: legacyHashKey(raw),
		}},
	}

	if !validateApiKey(raw, "127.0.0.1") {
		t.Fatal("validateApiKey rejected valid legacy key")
	}
	migrated := config.AppConfig.ApiKeys[0].KeyHash
	if migrated == legacyHashKey(raw) {
		t.Fatal("legacy key hash was not migrated")
	}
	if !verifyAPIKeyHash(raw, migrated) {
		t.Fatal("migrated key hash does not verify")
	}
}

func TestValidateApiKeyAppliesIPWhitelist(t *testing.T) {
	raw := "clicd_sk_0123456789abcdef0123456789abcdef"
	hash, err := hashAPIKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	config.AppConfig = &config.ClicdConfig{
		ApiKeys: []config.ApiKeyConfig{{
			ID:          "key1",
			Name:        "test",
			KeyHash:     hash,
			IPWhitelist: "192.0.2.10",
		}},
	}

	if validateApiKey(raw, "198.51.100.10") {
		t.Fatal("validateApiKey allowed disallowed IP")
	}
	if !validateApiKey(raw, "192.0.2.10") {
		t.Fatal("validateApiKey rejected allowed IP")
	}
}
