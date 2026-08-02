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
