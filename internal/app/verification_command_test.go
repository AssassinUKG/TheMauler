package app

import "testing"

func TestVerificationCommandEvidenceRequiresCheckExitStatus(t *testing.T) {
	for _, tc := range []struct {
		command        string
		check, compile bool
	}{
		{"go build ./...", true, true},
		{"cd '/tmp/project space' && go build ./main.go && echo BUILD_OK", true, true},
		{"go vet ./main.go 2>&1", true, true},
		{"go test ./... > test.log 2>&1", true, true},
		{"npm run check-types", true, true},
		{"wc -l generated/long.txt && tail -1 generated/long.txt", true, false},
		{"wc -l generated/long.txt && tail -n 3 generated/long.txt && head -n 1 generated/long.txt", true, false},
		{"echo 'go build passed'", false, false},
		{"cat compile-error-fixed.txt", false, false},
		{"go build ./... || true", false, false},
		{"go vet ./main.go 2>&1 || true; echo OK", false, false},
		{"go build ./...; echo BUILD_OK", false, false},
		{"go build ./...\necho BUILD_OK", false, false},
		{"go build ./... | tee build.log", false, false},
		{"! go build ./...", false, false},
		{"(go build ./... || true)", false, false},
		{"echo $(go build ./...)", false, false},
		{"bash -c 'go build ./... || true'", false, false},
		{"go build ./... &", false, false},
		{"go build ./... &&", false, false},
		{"go build -n ./...", false, false},
		{"go test -list . ./...", false, false},
		{"go build --help", false, false},
		{"exit 0 && go build ./...", false, false},
		{"exec true && go build ./...", false, false},
		{"trap 'exit 0' ERR && go build ./...", false, false},
	} {
		t.Run(tc.command, func(t *testing.T) {
			check, compile := verificationCommandEvidence(tc.command)
			if check != tc.check || compile != tc.compile {
				t.Fatalf("got check=%t compile=%t, want %t %t", check, compile, tc.check, tc.compile)
			}
		})
	}
}
