package cli

import (
	"strings"
	"testing"
)

func TestSafeReleaseBackupComponent(t *testing.T) {
	tests := map[string]string{
		"v1.2.3":              "1.2.3",
		" release/candidate ": "release_candidate",
		"../../etc/passwd":    "etc_passwd",
		"":                    "unknown",
	}
	for input, want := range tests {
		if got := safeReleaseBackupComponent(input); got != want {
			t.Fatalf("safeReleaseBackupComponent(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCopyFileToBackupRejectsUnsafeFileName(t *testing.T) {
	unsafeNames := []string{
		"../clicd",
		"..\\clicd",
		"subdir/clicd",
		"",
	}
	for _, name := range unsafeNames {
		if _, err := copyFileToBackup("missing-source", name, 0755); err == nil || !strings.Contains(err.Error(), "unsafe backup file name") {
			t.Fatalf("copyFileToBackup(%q) error = %v, want unsafe backup file name", name, err)
		}
	}
}

func TestFormatSSHAccessDoesNotExposePassword(t *testing.T) {
	out := formatSSHAccess(2222)
	if strings.Contains(out, "/") {
		t.Fatalf("formatSSHAccess output contains credential separator: %q", out)
	}
	if strings.Contains(strings.ToLower(out), "password123") {
		t.Fatalf("formatSSHAccess output exposed password: %q", out)
	}
	if !strings.Contains(out, "2222 -> 22") {
		t.Fatalf("formatSSHAccess output = %q, want SSH port mapping", out)
	}
}

func TestFormatSSHAccessHandlesMissingPort(t *testing.T) {
	out := formatSSHAccess(0)
	if !strings.Contains(out, "端口未分配") {
		t.Fatalf("formatSSHAccess output = %q, want missing port message", out)
	}
}
