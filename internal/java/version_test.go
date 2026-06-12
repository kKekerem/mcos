package java

import "testing"

func TestRequiredJavaMajor(t *testing.T) {
	cases := []struct {
		version string
		want    int
	}{
		{"1.8.8", 8},
		{"1.12.2", 8},
		{"1.16.5", 8},
		{"1.17", 17},
		{"1.17.1", 17},
		{"1.18.2", 17},
		{"1.19.4", 17},
		{"1.20.4", 17},
		{"1.20.5", 21},
		{"1.20.6", 21},
		{"1.21", 21},
		{"1.21.4", 21},
		{"1.21-rc1", 21},
		{"", 21},
		{"garbage", 21},
	}
	for _, c := range cases {
		if got := RequiredJavaMajor(c.version); got != c.want {
			t.Errorf("RequiredJavaMajor(%q) = %d, want %d", c.version, got, c.want)
		}
	}
}
