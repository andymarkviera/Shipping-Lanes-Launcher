package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestUninstallKeepsUnmanagedData(t *testing.T) {
	root := t.TempDir()
	game := filepath.Join(root, "builds")
	managed := filepath.Join(game, "21")
	unrelated := filepath.Join(game, "22")
	save := filepath.Join(root, "Godot", "app_userdata", "Shipping Lanes", "test.save")
	for _, path := range []string{managed, unrelated, filepath.Dir(save)} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	receipt, err := json.Marshal(installed{Asset: 21, Tag: "test", Entry: "ShippingLanes.exe"})
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []struct {
		path string
		data []byte
	}{
		{filepath.Join(managed, ".shipping-lanes-build.json"), receipt},
		{filepath.Join(managed, "ShippingLanes.exe"), []byte("fixture")},
		{filepath.Join(unrelated, "keep.txt"), []byte("unrelated")},
		{save, []byte("save data")},
	} {
		if err := os.WriteFile(file.path, file.data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	l := &launcher{root: game, active: findInstalled(game)}
	l.action("uninstall")
	l.installs.Wait()
	if _, err := os.Stat(managed); !os.IsNotExist(err) {
		t.Fatal("managed game files were not removed")
	}
	for _, path := range []string{unrelated, save} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("uninstall removed unrelated data", err)
		}
	}
	if l.snapshot().Installed != "" {
		t.Fatal("launcher still reports an installed game")
	}
}
