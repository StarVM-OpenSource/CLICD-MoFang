package lxc

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRootfsCommandAddsSeparatorAndPreservesArgs(t *testing.T) {
	base := t.TempDir()
	rootfs := filepath.Join(base, "ct-1", "rootfs")
	if err := os.MkdirAll(rootfs, 0755); err != nil {
		t.Fatal(err)
	}

	m := &Manager{LxcPath: base}
	cmd, err := m.rootfsCommand(rootfs, "sh", "-c", "true", "--flag")
	if err != nil {
		t.Fatalf("rootfsCommand returned error: %v", err)
	}

	want := []string{"chroot", "--", rootfs, "sh", "-c", "true", "--flag"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("cmd.Args = %#v, want %#v", cmd.Args, want)
	}
}

func TestRootfsCommandAllowsLeadingDashContainerName(t *testing.T) {
	base := t.TempDir()
	rootfs := filepath.Join(base, "-ct", "rootfs")
	if err := os.MkdirAll(rootfs, 0755); err != nil {
		t.Fatal(err)
	}

	m := &Manager{LxcPath: base}
	cmd, err := m.rootfsCommand(rootfs, "true")
	if err != nil {
		t.Fatalf("rootfsCommand returned error: %v", err)
	}

	want := []string{"chroot", "--", rootfs, "true"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("cmd.Args = %#v, want %#v", cmd.Args, want)
	}
}

func TestRootfsCommandRejectsUnsafeRootfsPaths(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	m := &Manager{LxcPath: base}

	tests := []struct {
		name string
		path string
	}{
		{name: "outside base", path: filepath.Join(outside, "ct-1", "rootfs")},
		{name: "base path", path: base},
		{name: "not rootfs", path: filepath.Join(base, "ct-1", "not-rootfs")},
		{name: "rootfs directly under base", path: filepath.Join(base, "rootfs")},
		{name: "relative rootfs", path: filepath.Join("ct-1", "rootfs")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := m.rootfsCommand(tc.path, "true"); err == nil {
				t.Fatalf("rootfsCommand(%q) returned nil error", tc.path)
			}
		})
	}
}

func TestSafeRootfsPathRejectsSiblingPrefix(t *testing.T) {
	parent := t.TempDir()
	base := filepath.Join(parent, "lxc")
	siblingRootfs := filepath.Join(parent, "lxc-evil", "ct-1", "rootfs")
	m := &Manager{LxcPath: base}

	if _, err := m.safeRootfsPath(siblingRootfs); err == nil || !strings.Contains(err.Error(), "unsafe rootfs path") {
		t.Fatalf("safeRootfsPath returned %v, want unsafe rootfs path error", err)
	}
}
