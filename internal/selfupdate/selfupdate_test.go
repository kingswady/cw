package selfupdate

import "testing"

func TestReleaseOrder(t *testing.T) {
	cases := map[[2]string]bool{
		{"v0.3.0", "v0.2.9"}: true, {"v0.10.0", "v0.9.0"}: true, {"v1.0.0", "v0.99.99"}: true,
		{"v0.2.0", "v0.2.0"}: false, {"v0.2.0", "v0.3.0"}: false, {"v0.3.0", "v0.3.0-rc1"}: false,
	}
	for pair, want := range cases {
		if got := Newer(pair[0], pair[1]); got != want {
			t.Errorf("Newer(%s, %s) = %v", pair[0], pair[1], got)
		}
	}
}
