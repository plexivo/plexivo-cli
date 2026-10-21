package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSaveCreatesAndReplacesPrivateLogin(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plexivo")
	p := filepath.Join(dir, "config.json")
	if err := saveAt(p, Config{Token: "fixture-first"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := saveAt(p, Config{Token: "fixture-replacement"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var saved Config
	if err := json.Unmarshal(b, &saved); err != nil || saved.Token != "fixture-replacement" {
		t.Fatal("saved login does not match")
	}
	if runtime.GOOS != "windows" {
		for name, mode := range map[string]os.FileMode{p: 0o600, dir: 0o700} {
			info, err := os.Stat(name)
			if err != nil || info.Mode().Perm() != mode {
				t.Fatal("login permissions are not private")
			}
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "config.json" {
		t.Fatal("temporary login file was left behind")
	}
}

func TestSaveRefusesNonRegularTargetWithoutChangingIt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plexivo")
	p := filepath.Join(dir, "config.json")
	if err := os.MkdirAll(p, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := saveAt(p, Config{Token: "fixture"}); err == nil {
		t.Fatal("accepted directory as a login file")
	}
	if info, err := os.Stat(p); err != nil || !info.IsDir() {
		t.Fatal("existing target was changed")
	}
}

func TestSaveDoesNotFollowLoginSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires a platform-specific Windows privilege")
	}
	root := t.TempDir()
	out := filepath.Join(root, "unchanged")
	if err := os.WriteFile(out, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "config.json")
	if err := os.Symlink(out, p); err != nil {
		t.Fatal(err)
	}
	if err := saveAt(p, Config{Token: "fixture"}); err == nil {
		t.Fatal("accepted a symlink as the login file")
	}
	b, err := os.ReadFile(out)
	if err != nil || string(b) != "original" {
		t.Fatal("symlink destination was changed")
	}
}

func TestSaveDoesNotFollowLoginDirectorySymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires a platform-specific Windows privilege")
	}
	root := t.TempDir()
	target := filepath.Join(root, "elsewhere")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "plexivo")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := saveAt(filepath.Join(link, "config.json"), Config{Token: "fixture"}); err == nil {
		t.Fatal("accepted a symlink as the login directory")
	}
	if _, err := os.Stat(filepath.Join(target, "config.json")); !os.IsNotExist(err) {
		t.Fatal("wrote a login through the directory symlink")
	}
}
