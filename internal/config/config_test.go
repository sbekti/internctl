package config

import "testing"

func TestNormalizeProfileName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "default when blank", input: "", want: DefaultProfile},
		{name: "trimmed valid", input: " dev-profile ", want: "dev-profile"},
		{name: "reject path traversal", input: "../prod", wantErr: true},
		{name: "reject slash", input: "prod/test", wantErr: true},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := NormalizeProfileName(test.input)
			if test.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeProfileName returned error: %v", err)
			}
			if got != test.want {
				t.Fatalf("NormalizeProfileName = %q, want %q", got, test.want)
			}
		})
	}
}
