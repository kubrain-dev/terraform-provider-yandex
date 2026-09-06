package provider

import "strings"

// These are the pure YC->Kubrain mapping functions. They are deliberately free
// of any Terraform or HTTP types so they can be unit-tested in isolation — the
// mapping semantics are the interesting, reviewable part of the provider.

// aclToPublic maps a Yandex/AWS-style storage ACL to Kubrain's single boolean
// `public` (anonymous read). Any "public-read"-family ACL means public; every
// other value (including "private", "", "authenticated-read") means private.
func aclToPublic(acl string) bool {
	switch strings.ToLower(strings.TrimSpace(acl)) {
	case "public-read", "public-read-write", "public":
		return true
	default:
		return false
	}
}

// masterToTier maps a yandex_kubernetes_cluster master block to a Kubrain tier.
// A zonal master (single control plane) maps to `normal` (one dedicated control
// plane + workers); a regional master (3 control planes across zones) maps to
// `ha`. When neither is set we default to `normal`, the closest single-master
// analog. hasZonal/hasRegional are whether those blocks are present in config.
func masterToTier(hasZonal, hasRegional bool) string {
	if hasRegional {
		return "ha"
	}
	return "normal"
}

// memoryToRAMGiB converts a YC node memory value (float GiB, the YC unit) to
// Kubrain's integer GiB, rounding to the nearest whole GiB with a floor of 1.
// A zero/negative input yields 0 so the caller can fall back to a default.
func memoryToRAMGiB(memoryGiB float64) int {
	if memoryGiB <= 0 {
		return 0
	}
	r := int(memoryGiB + 0.5)
	if r < 1 {
		r = 1
	}
	return r
}
