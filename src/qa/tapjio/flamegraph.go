package tapjio

import (
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"qa/tapjio/assets"
	"runtime"
)

// make directory ~/.qa/support/
// write flamegraph.pl to ~/.qa/support/flamegraph.pl
// use flamegraph.pl to generate flamegraph svg from ruby stack samples
// write ~/.qa/audit/2016/04/11/qklwnepd/flamegraph.svg
// write ~/.qa/audit/2016/04/11/qklwnepd/trace.html
// write ~/.qa/audit/2016/04/11/qklwnepd/results.tapj
//

func emitSupportAsset(assetName string) (string, error) {
	var home string
	if runtime.GOOS == "windows" {
		home = os.Getenv("USERPROFILE")
	} else {
		home = os.Getenv("HOME")
	}

	assetPath := path.Join(home, ".qa", "support", assetName)
	assetDir := filepath.Dir(assetPath)
	err := os.MkdirAll(assetDir, 0755)
	if err != nil {
		return "", err
	}

	assetData, err := assets.Asset(assetName)
	if err != nil {
		return "", err
	}

	f, err := os.Create(assetPath)
	if err != nil {
		return "", err
	}

	defer f.Close()
	_, err = f.Write(assetData)
	if err != nil {
		return "", err
	}

	return assetPath, nil
}

// GenerateFlameGraph runs the flamegraph script to generate a flame graph SVG.
func GenerateFlameGraph(stacktraceReader io.Reader, writer io.WriteCloser, args ...string) error {
	flamegraphPl, err := emitSupportAsset("flamegraph.pl")
	if err != nil {
		return err
	}

	defer writer.Close()

	cmd := exec.Command("perl", append([]string{flamegraphPl}, args...)...)
	cmd.Stdin = stacktraceReader
	cmd.Stderr = os.Stderr
	cmd.Stdout = writer
	return cmd.Run()
}

func DiffFoldedStacktraces(file1 string, file2 string, writer io.Writer, args ...string) error {
	difffoldedPl, err := emitSupportAsset("difffolded.pl")
	if err != nil {
		return err
	}

	cmd := exec.Command("perl",
		append([]string{
			difffoldedPl,
			file1,
			file2,
		}, args...)...)

	cmd.Stdin = nil
	cmd.Stderr = os.Stderr
	cmd.Stdout = writer
	return cmd.Run()
}
