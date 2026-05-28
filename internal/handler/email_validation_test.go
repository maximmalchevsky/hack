package handler

import "testing"

func TestLooksLikeEmail(t *testing.T) {
	valid := []string{
		"a@b.co",
		"maxim@iqj.app",
		"user.name@example.com",
		"x@worktime.local",
	}
	invalid := []string{
		"",
		"no-at-sign",
		"@nodomain.com",
		"trailing@",
		"a@b",
		"a@.co",
		"a@b.",
		"with space@b.co",
		"a@b,c.com",
	}
	for _, e := range valid {
		if !looksLikeEmail(e) {
			t.Errorf("looksLikeEmail(%q) = false, want true", e)
		}
	}
	for _, e := range invalid {
		if looksLikeEmail(e) {
			t.Errorf("looksLikeEmail(%q) = true, want false", e)
		}
	}
}
