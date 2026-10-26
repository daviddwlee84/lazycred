package version

import "testing"

func TestVersionPrecedence(t *testing.T) {
	for _, v := range []struct{ injected, module, want string }{{"v0.1.0", "v0.0.9", "v0.1.0"}, {"dev", "v0.1.0", "v0.1.0"}, {"", "(devel)", "dev"}} {
		if got := Resolve(v.injected, v.module); got != v.want {
			t.Fatalf("got %s", got)
		}
	}
}
