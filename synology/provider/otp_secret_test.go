package provider

import (
	"strings"
	"testing"
)

func TestValidateOtpSecret(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		secret  string
		wantErr bool
	}{
		{"16 chars", "JBSWY3DPEHPK3PXP", false},
		{"32 chars", strings.Repeat("JBSWY3DP", 4), false},
		{"52 chars (longer DSM secret)", strings.Repeat("A", 52), false},
		{"too short", "JBSWY3DP", true},
		{"too long", strings.Repeat("A", 129), true},
		{"lowercase", "jbswy3dpehpk3pxp", true},
		{"otpauth URL", "otpauth://totp/x?secret=JBSWY3DPEHPK3PXP", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := validateOtpSecret(tt.secret); (err != nil) != tt.wantErr {
				t.Errorf("validateOtpSecret(%q) error = %v, wantErr %v", tt.secret, err, tt.wantErr)
			}
		})
	}
}
