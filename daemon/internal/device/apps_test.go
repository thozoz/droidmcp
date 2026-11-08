package device

import (
	"strings"
	"testing"
)

func TestMatchApp(t *testing.T) {
	apps := []App{
		{Package: "com.android.settings", Activity: "com.android.settings.Settings", Label: "Settings"},
		{Package: "com.google.android.gms", Label: "Google Settings"},
		{Package: "com.whatsapp", Label: "WhatsApp"},
		{Package: "com.example.camera", Label: "Kamera"},
		{Package: "com.example.notes", Label: "Işık Notları"},
		{Package: "com.example.dup1", Label: "Calculator"},
		{Package: "com.example.dup2", Label: "Calculator"},
	}

	tests := []struct {
		name      string
		target    string
		wantPkg   string
		wantErr   string
		ambiguous bool
	}{
		{
			name:    "exact label match beats substring match",
			target:  "Settings",
			wantPkg: "com.android.settings",
		},
		{
			name:    "case-insensitive exact label match",
			target:  "settings",
			wantPkg: "com.android.settings",
		},
		{
			name:    "case-insensitive exact label match with Turkish query",
			target:  "kamera",
			wantPkg: "com.example.camera",
		},
		{
			name:    "exact package match",
			target:  "com.whatsapp",
			wantPkg: "com.whatsapp",
		},
		{
			name:    "case-insensitive label contains",
			target:  "Whats",
			wantPkg: "com.whatsapp",
		},
		{
			name:    "Turkish label contains with Işık",
			target:  "Notları",
			wantPkg: "com.example.notes",
		},
		{
			name:    "fallback to package substring when label doesn't match",
			target:  "whatsapp",
			wantPkg: "com.whatsapp",
		},
		{
			name:      "ambiguous exact label",
			target:    "Calculator",
			wantErr:   `"Calculator" is ambiguous: Calculator (com.example.dup1), Calculator (com.example.dup2)`,
			ambiguous: true,
		},
		{
			name:      "ambiguous substring match",
			target:    "Setting",
			wantErr:   `"Setting" is ambiguous: Settings (com.android.settings), Google Settings (com.google.android.gms)`,
			ambiguous: true,
		},
		{
			name:    "no match",
			target:  "NonExistentApp",
			wantErr: `no installed app matches "NonExistentApp"`,
		},
		{
			name:    "empty target",
			target:  "   ",
			wantErr: "empty target",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MatchApp(apps, tt.target)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Package != tt.wantPkg {
				t.Fatalf("got pkg %q, want %q", got.Package, tt.wantPkg)
			}
		})
	}
}
