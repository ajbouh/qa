package flamegraph

import (
	"bytes"
	"flag"
	"io"
	"io/ioutil"
	"os"

	"qa/cmd"
	"qa/tapjio"
)

func decodeStacktrace(path string, def io.ReadCloser, writer io.Writer) error {
	var reader io.ReadCloser
	var err error
	if path == "-" {
		reader = def
	} else {
		reader, err = os.Open(path)
		if err != nil {
			return err
		}
	}
	defer reader.Close()

	return tapjio.DecodeReader(reader, tapjio.NewStacktraceEmitter(writer))
}

// Usage:
//     flamegraph
//     flamegraph in.tapj [ -- ... ]
//     flamegraph in.tapj out.svg [ -- ... ]
//     flamegraph in1.tapj in2.tapj out.svg [ -- ... ]

func Main(env *cmd.Env, argv []string) error {
	flags := flag.NewFlagSet(argv[0], flag.ContinueOnError)

	err := flags.Parse(argv[1:])
	if err != nil {
		return err
	}

	var remainingArgs []string
	var flamegraphArgs []string
	foundArgSep := false
	for _, arg := range flags.Args() {
		if foundArgSep {
			flamegraphArgs = append(flamegraphArgs, arg)
		} else if arg == "--" {
			foundArgSep = true
		} else {
			remainingArgs = append(remainingArgs, arg)
		}
	}

	input := "-"
	output := "-"
	diffInputA := ""
	diffInputB := ""

	var writer io.Writer

	switch {
	case len(remainingArgs) == 1:
		input = remainingArgs[0]
	case len(remainingArgs) == 2:
		input = remainingArgs[0]
		output = remainingArgs[1]
	case len(remainingArgs) == 3:
		diffInputA = remainingArgs[0]
		diffInputB = remainingArgs[1]
		input = ""
		output = remainingArgs[2]
	}

	stdinCloser := ioutil.NopCloser(env.Stdin)
	var stacktraceBytes bytes.Buffer
	if diffInputA != "" || diffInputB != "" {
		stacktraceAFile, err := ioutil.TempFile("", "stacktrace")
		if err != nil {
			return err
		}
		defer os.Remove(stacktraceAFile.Name())
		stacktraceBFile, err := ioutil.TempFile("", "stacktrace")
		if err != nil {
			return err
		}
		defer os.Remove(stacktraceBFile.Name())

		if err := decodeStacktrace(diffInputA, stdinCloser, stacktraceAFile); err != nil {
			return err
		}
		if err := stacktraceAFile.Close(); err != nil {
			return err
		}
		if err := decodeStacktrace(diffInputB, stdinCloser, stacktraceBFile); err != nil {
			return err
		}
		if err := stacktraceBFile.Close(); err != nil {
			return err
		}

		if err := tapjio.DiffFoldedStacktraces(
			stacktraceAFile.Name(),
			stacktraceBFile.Name(),
			&stacktraceBytes); err != nil {
			return err
		}
	} else {
		if err := decodeStacktrace(input, stdinCloser, &stacktraceBytes); err != nil {
			return err
		}
	}

	if output == "-" {
		writer = env.Stdout
	} else {
		var file *os.File
		file, err = os.Create(output)
		if err != nil {
			return err
		}
		writer = file
		defer file.Close()
	}

	err = tapjio.GenerateFlameGraph(bytes.NewReader(stacktraceBytes.Bytes()), writer, flamegraphArgs...)
	if err != nil {
		return err
	}

	return nil
}
