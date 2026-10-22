package realtime

import (
	"testing"
	"time"
)

// Expected tokens were produced by LcobucciJwtProvider in the shlinkio/shlink:5.1.8 image, with Chronos' test now set
// to the same instant and the same (fake) secret.
func TestMercureTokensMatchUpstream(t *testing.T) {
	secret := "fake-mercure-secret-for-tests-0123456789"
	m := NewMercure(MercureOptions{Enabled: true, JWTSecret: &secret, JWTIssuer: "Shlink"})
	m.Now = func() time.Time { return time.Unix(1700000000, 0) }

	publish, err := m.BuildPublishToken()
	if err != nil {
		t.Fatal(err)
	}
	if want := "eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJpc3MiOiJTaGxpbmsiLCJpYXQiOjE3MDAwMDAwMDAsImV4cCI6MTcwMDAwMDYwMCwibWVyY3VyZSI6eyJwdWJsaXNoIjpbIioiXX19.7xUkgJYAk1kH-TahTtzgjbe72Z1xKEIN_W6FYEfxBxc"; publish != want { // gitleaks:allow (fake secret, expired token)
		t.Errorf("publish token:\n got %s\nwant %s", publish, want)
	}
	subscribe, err := m.BuildSubscriptionToken(time.Unix(1700086400, 0))
	if err != nil {
		t.Fatal(err)
	}
	if want := "eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJpc3MiOiJTaGxpbmsiLCJpYXQiOjE3MDAwMDAwMDAsImV4cCI6MTcwMDA4NjQwMCwibWVyY3VyZSI6eyJzdWJzY3JpYmUiOlsiKiJdfX0.RhTGfCLHseKXvxTtebG8e2cAU6GYz-2OT4M1K4kLtG8"; subscribe != want { // gitleaks:allow (fake secret, expired token)
		t.Errorf("subscription token:\n got %s\nwant %s", subscribe, want)
	}

	short := "short"
	m.Options.JWTSecret = &short
	if _, err := m.BuildPublishToken(); err == nil || err.Error() != "Key provided is shorter than 256 bits, only 40 bits provided" {
		t.Errorf("unexpected error for short key: %v", err)
	}
}
