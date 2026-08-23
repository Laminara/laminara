package prepare

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestE2EServerJava(t *testing.T) {
	if os.Getenv("LAMINARA_E2E") == "" {
		t.Skip("E2E: set LAMINARA_E2E=1 to download a real Mojang JRE and run java -version")
	}
	preparer := NewPreparer()
	dir := t.TempDir()

	java, err := preparer.serverJavaBin(context.Background(), dir, "java-runtime-gamma")
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(java, "-version").CombinedOutput()
	if err != nil {
		t.Fatalf("java -version failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "version") {
		t.Fatalf("unexpected java -version output:\n%s", output)
	}
	t.Logf("server java: %s\n%s", java, output)
}
