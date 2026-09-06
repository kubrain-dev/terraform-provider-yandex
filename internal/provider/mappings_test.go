package provider

import "testing"

func TestACLToPublic(t *testing.T) {
	cases := map[string]bool{
		"public-read":               true,
		"public-read-write":         true,
		"public":                    true,
		"PUBLIC-READ":               true,
		"  public-read  ":           true,
		"private":                   false,
		"":                          false,
		"authenticated-read":        false,
		"bucket-owner-full-control": false,
	}
	for acl, want := range cases {
		if got := aclToPublic(acl); got != want {
			t.Errorf("aclToPublic(%q) = %v, want %v", acl, got, want)
		}
	}
}

func TestMasterToTier(t *testing.T) {
	cases := []struct {
		hasZonal, hasRegional bool
		want                  string
	}{
		{true, false, "normal"},  // zonal master -> single dedicated CP
		{false, true, "ha"},      // regional master -> 3 CPs
		{false, false, "normal"}, // neither -> default to single CP
		{true, true, "ha"},       // regional wins if both somehow set
	}
	for _, c := range cases {
		if got := masterToTier(c.hasZonal, c.hasRegional); got != c.want {
			t.Errorf("masterToTier(zonal=%v, regional=%v) = %q, want %q", c.hasZonal, c.hasRegional, got, c.want)
		}
	}
}

func TestMemoryToRAMGiB(t *testing.T) {
	cases := []struct {
		in   float64
		want int
	}{
		{0, 0},   // unset -> caller falls back to default
		{-4, 0},  // invalid -> 0
		{2, 2},   // exact
		{16, 16}, // exact
		{7.4, 7}, // round down
		{7.6, 8}, // round up
		{0.4, 1}, // floor at 1 for any positive memory
		{0.9, 1}, // rounds to 1
	}
	for _, c := range cases {
		if got := memoryToRAMGiB(c.in); got != c.want {
			t.Errorf("memoryToRAMGiB(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}
