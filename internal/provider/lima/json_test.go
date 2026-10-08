package lima

import (
	"testing"

	"github.com/apomonosi/sandboxing/internal/provider"
)

func TestParseLimaList_JSONArray(t *testing.T) {
	got, err := parseLimaList([]byte(`[{"name":"demo","status":"Running"},{"name":"other","status":"Stopped"}]`))
	if err != nil {
		t.Fatalf("parseLimaList: %v", err)
	}
	if len(got) != 2 || got[0].Name != "demo" || got[1].Name != "other" {
		t.Errorf("parseLimaList() = %+v, want two instances named demo/other", got)
	}
}

func TestParseLimaList_JSONLines(t *testing.T) {
	got, err := parseLimaList([]byte("{\"name\":\"demo\",\"status\":\"Running\"}\n{\"name\":\"other\",\"status\":\"Stopped\"}\n"))
	if err != nil {
		t.Fatalf("parseLimaList: %v", err)
	}
	if len(got) != 2 || got[0].Name != "demo" || got[1].Name != "other" {
		t.Errorf("parseLimaList() = %+v, want two instances named demo/other", got)
	}
}

// TestParseLimaList_EffectiveMounts uses a line of real `limactl list
// --json` output (Lima v2.2.1, trimmed) for an instance created from the
// stock template:ubuntu-lts with no reset of the template's mounts.
func TestParseLimaList_EffectiveMounts(t *testing.T) {
	line := `{"name":"probe","hostname":"lima-probe","status":"Stopped","dir":"/home/u/.lima/probe","vmType":"qemu","arch":"x86_64",` +
		`"config":{"vmType":"qemu","mounts":[{"location":"/home/u","mountPoint":"/home/u","writable":false,` +
		`"sshfs":{"cache":true,"followSymlinks":false,"sftpDriver":""},"9p":{"securityModel":"none","protocolVersion":"9p2000.L","msize":"128KiB","cache":"fscache"}}],` +
		`"propagateProxyEnv":true},"protected":false,"limaVersion":"2.2.1"}` + "\n"
	got, err := parseLimaList([]byte(line))
	if err != nil {
		t.Fatalf("parseLimaList: %v", err)
	}
	if len(got) != 1 || got[0].Name != "probe" || got[0].Dir != "/home/u/.lima/probe" {
		t.Fatalf("parseLimaList() = %+v, want the one instance \"probe\" with its dir", got)
	}
	if got[0].Config == nil || len(got[0].Config.Mounts) != 1 {
		t.Fatalf("Config = %+v, want one mount", got[0].Config)
	}
	m := got[0].Config.Mounts[0]
	location, mountPoint, writable := resolveMount(m)
	if m.MountPoint == nil || m.Writable == nil || location != "/home/u" || mountPoint != "/home/u" || writable {
		t.Errorf("mount = %q at %q (writable %t, mountPoint/writable present: %t/%t), want /home/u read-only at /home/u",
			location, mountPoint, writable, m.MountPoint != nil, m.Writable != nil)
	}
}

func TestParseLimaList_Empty(t *testing.T) {
	got, err := parseLimaList([]byte(""))
	if err != nil {
		t.Fatalf("parseLimaList: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("parseLimaList(\"\") = %v, want empty", got)
	}
}

func TestToInstance_DefensiveOnMalformedFields(t *testing.T) {
	inst := toInstance(limaInstanceJSON{Name: "demo", Status: "Running", CreatedAt: "not-a-timestamp"})
	if inst.Name != "demo" {
		t.Errorf("Name = %q, want demo", inst.Name)
	}
	if inst.Status != provider.StatusRunning {
		t.Errorf("Status = %v, want Running", inst.Status)
	}
	if !inst.CreatedAt.IsZero() {
		t.Errorf("CreatedAt = %v, want zero value for an unparseable timestamp", inst.CreatedAt)
	}
	if inst.Provider != "lima" {
		t.Errorf("Provider = %q, want lima", inst.Provider)
	}
}

func TestToInstanceStatus(t *testing.T) {
	cases := map[string]provider.InstanceStatus{
		"Running": provider.StatusRunning,
		"running": provider.StatusRunning,
		"Stopped": provider.StatusStopped,
		"Broken":  provider.StatusUnknown,
		"":        provider.StatusUnknown,
	}
	for in, want := range cases {
		if got := toInstanceStatus(in); got != want {
			t.Errorf("toInstanceStatus(%q) = %v, want %v", in, got, want)
		}
	}
}
