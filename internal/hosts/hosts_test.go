package hosts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHosts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HIVE_CONFIG", filepath.Join(dir, "config"))
	if hs, err := List(); err != nil || len(hs) != 0 {
		t.Fatalf("no file: %v, %v", hs, err)
	}
	if err := Add(Host{Label: "box", Address: "https://box.example.ts.net"}); err != nil {
		t.Fatal(err)
	}
	if err := Add(Host{Label: "lab", Address: "lab.example.ts.net"}); err != nil {
		t.Fatal(err)
	}
	if err := Add(Host{Label: "BOX", Address: "http://100.64.0.1:4483"}); err != nil {
		t.Fatal(err)
	}
	h, ok, err := Find("Box")
	if err != nil || !ok || h.Address != "http://100.64.0.1:4483" {
		t.Fatalf("Find after replacing: %+v %v %v", h, ok, err)
	}
	if hs, _ := List(); len(hs) != 2 {
		t.Fatalf("hosts = %+v, want 2", hs)
	}
	if fi, err := os.Stat(Path()); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("hosts.json: %v, %v", fi.Mode(), err)
	}
	if ok, err := Remove("lab"); !ok || err != nil {
		t.Fatalf("Remove: %v %v", ok, err)
	}
	if ok, _ := Remove("lab"); ok {
		t.Fatal("removed twice")
	}
}

func TestHostsRefuse(t *testing.T) {
	t.Setenv("HIVE_CONFIG", filepath.Join(t.TempDir(), "config"))
	for _, h := range []Host{
		{Label: "", Address: "https://x.ts.net"},
		{Label: "a/b", Address: "https://x.ts.net"},
		{Label: "box.ts", Address: "https://x.ts.net"}, // would read as a host name
		{Label: "root@box", Address: "https://x.ts.net"},
		{Label: "box", Address: "https://root@x.ts.net"}, // accounts aren't supported
		{Label: "box", Address: "ftp://x"},
		{Label: "box", Address: "/tmp/x.sock"},
	} {
		if err := Add(h); err == nil {
			t.Errorf("Add(%+v) accepted", h)
		}
	}
}
