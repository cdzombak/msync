package main

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"msync/cli"
)

func TestMakeMusicTree_SymlinkCycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink tests are not supported on windows")
	}

	root := filepath.Join(t.TempDir(), "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := ioutil.WriteFile(filepath.Join(root, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.Symlink(root, filepath.Join(root, "loop")); err != nil {
		if os.IsPermission(err) {
			t.Skipf("symlink not permitted: %v", err)
		}
		t.Fatalf("symlink: %v", err)
	}

	ctx := cli.WithCLIOut(context.Background())
	tree, err := MakeMusicTree(ctx, root)
	if err != nil {
		t.Fatalf("MakeMusicTree: %v", err)
	}
	if got := tree.CountNodes(); got != 2 {
		t.Fatalf("CountNodes = %d, want 2", got)
	}
}

func TestMakeMusicTree_FollowsSymlinkDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink tests are not supported on windows")
	}

	root := filepath.Join(t.TempDir(), "root")
	realDir := filepath.Join(root, "real")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := ioutil.WriteFile(filepath.Join(realDir, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.Symlink(realDir, filepath.Join(root, "alias")); err != nil {
		if os.IsPermission(err) {
			t.Skipf("symlink not permitted: %v", err)
		}
		t.Fatalf("symlink: %v", err)
	}

	ctx := cli.WithCLIOut(context.Background())
	tree, err := MakeMusicTree(ctx, root)
	if err != nil {
		t.Fatalf("MakeMusicTree: %v", err)
	}
	if got := tree.CountNodes(); got != 5 {
		t.Fatalf("CountNodes = %d, want 5", got)
	}
}
