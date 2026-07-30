package provider

import "testing"

func TestCommand_String(t *testing.T) {
	cases := []struct {
		name string
		cmd  Command
		want string
	}{
		{
			name: "simple args need no quoting",
			cmd:  Command{Binary: "incus", Args: []string{"start", "demo"}},
			want: "incus start demo",
		},
		{
			name: "safe punctuation is left bare",
			cmd:  Command{Binary: "incus", Args: []string{"list", "--format", "json", "images:ubuntu/24.04"}},
			want: "incus list --format json images:ubuntu/24.04",
		},
		{
			name: "wildcard domain is left bare",
			cmd:  Command{Binary: "incus", Args: []string{"network", "acl", "rule", "add", "*.example.com"}},
			want: "incus network acl rule add *.example.com",
		},
		{
			name: "spaces get single-quoted",
			cmd:  Command{Binary: "incus", Args: []string{"exec", "demo", "--", "echo hello world"}},
			want: `incus exec demo -- 'echo hello world'`,
		},
		{
			name: "embedded single quotes are escaped",
			cmd:  Command{Binary: "incus", Args: []string{"exec", "demo", "--", "echo it's fine"}},
			want: `incus exec demo -- 'echo it'\''s fine'`,
		},
		{
			name: "empty arg renders as empty quotes",
			cmd:  Command{Binary: "incus", Args: []string{""}},
			want: "incus ''",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.cmd.String(); got != c.want {
				t.Errorf("String() = %q, want %q", got, c.want)
			}
		})
	}
}
