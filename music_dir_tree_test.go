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

// nfcName and nfdName are the same file name — "Communiqué" — in the two Unicode forms a local
// disk and an SMB share disagree about. nfc uses the single precomposed code point for "é";
// nfd uses "e" followed by a combining acute accent.
const (
	nfcName = "Communiqu\u00e9.flac"  // é as a single precomposed code point
	nfdName = "Communique\u0301.flac" // e followed by a combining acute accent
)

func TestNormalizeFileNameForComparing_UnicodeForms(t *testing.T) {
	if nfcName == nfdName {
		t.Fatal("test inputs must differ byte-for-byte to be a meaningful test")
	}
	nfc := normalizeFileNameForComparing(nfcName)
	nfd := normalizeFileNameForComparing(nfdName)
	if nfc != nfd {
		t.Errorf("normalized forms differ: NFC input -> %q, NFD input -> %q; want equal", nfc, nfd)
	}
}

func TestNormalizeFileNameForComparing_LowercasesAndStripsMusicExt(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Track.MP3", "track"},
		{"Track.m4a", "track"},
		{"Cover.JPG", "cover.jpg"}, // not a music file, so the extension stays
	} {
		if got := normalizeFileNameForComparing(tc.in); got != tc.want {
			t.Errorf("normalizeFileNameForComparing(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestMakeMusicTree_MatchesAcrossUnicodeForms is the regression test for files being deleted and
// recopied on every run: a name stored decomposed in one tree must resolve to the node built from
// the precomposed name in the other. The two names go in separate trees because APFS treats them
// as the same file within a single directory.
func TestMakeMusicTree_MatchesAcrossUnicodeForms(t *testing.T) {
	build := func(fileName string) *MusicTreeNode {
		t.Helper()
		root := filepath.Join(t.TempDir(), "root")
		if err := os.MkdirAll(filepath.Join(root, "Dire Straits"), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		// a non-music extension keeps the test hermetic: no afinfo, no bitrate lookup.
		if err := ioutil.WriteFile(filepath.Join(root, "Dire Straits", fileName+".txt"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		tree, err := MakeMusicTree(cli.WithCLIOut(context.Background()), root)
		if err != nil {
			t.Fatalf("MakeMusicTree: %v", err)
		}
		return tree
	}

	sourceTree := build(nfcName)
	destTree := build(nfdName)

	var sourceLeaf *MusicTreeNode
	_ = sourceTree.Walk(func(n *MusicTreeNode) error {
		if n.IsFile {
			sourceLeaf = n
		}
		return nil
	})
	if sourceLeaf == nil {
		t.Fatal("no file node found in the source tree")
	}
	if !destTree.HasNodeAtTreePath(sourceLeaf.TreePath) {
		t.Errorf("destination tree has no node at %q; the same file in a different Unicode form "+
			"would be treated as missing from source and deleted", sourceLeaf.TreePath)
	}
}

func TestMakeMusicTree_UnreadableMusicFile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// a truncated music file, as an interrupted transcode leaves behind: afinfo can't read it.
	if err := ioutil.WriteFile(filepath.Join(root, "truncated.m4a"), []byte("\x00\x00\x00\x20ftypM4A "), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	ctx := cli.WithCLIOut(context.Background())
	tree, err := MakeMusicTree(ctx, root)
	if err != nil {
		t.Fatalf("MakeMusicTree: %v", err)
	}
	node := tree.NodeAtTreePath([]string{"truncated"})
	if node == nil {
		t.Fatal("NodeAtTreePath(truncated) = nil, want the unreadable file's node")
	}
	if !node.BitrateUnreadable {
		t.Error("BitrateUnreadable = false, want true")
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
