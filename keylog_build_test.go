package dgproto_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestDefaultBuildExcludesKeylogSink checks a security-specific build boundary,
// not a package layout policy. Go resolves transitive dependencies and build
// constraints for the current platform; CI exercises the other supported OSes.
// The separate default-build Session test verifies that no keylog file is written.
func TestDefaultBuildExcludesKeylogSink(t *testing.T) {
	cmd := exec.Command("go", "list", "-tags=", "-deps", "-f", "{{.ImportPath}}", ".")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("list default-build dependencies: %v\n%s", err, output)
	}
	const sink = "github.com/datagram-messenger/dgproto-go/internal/keylog"
	for _, dependency := range strings.Fields(string(output)) {
		if dependency == sink {
			t.Fatal("default build includes the optional traffic-secret sink")
		}
	}
}
