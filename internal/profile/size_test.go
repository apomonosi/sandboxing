package profile

import "testing"

func TestParseSize(t *testing.T) {
	cases := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"4GiB", 4 * (1 << 30), false},
		{"20GB", 20_000_000_000, false},
		{"512MiB", 512 * (1 << 20), false},
		{"1024", 1024, false},
		{"", 0, true},
		{"0GiB", 0, true},
		{"-1GiB", 0, true},
		{"not-a-size", 0, true},
	}
	for _, c := range cases {
		got, err := ParseSize(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseSize(%q): expected error, got %d", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseSize(%q): unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseSize(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
