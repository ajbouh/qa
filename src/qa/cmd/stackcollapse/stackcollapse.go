package stackcollapse

import (
	"flag"
	"io"
	"os"

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

	return tapjio.Decode(reader, tapjio.NewStacktraceEmitter(writer))
}

// Usage:
//     stackcollapse
//     stackcollapse in.tapj
//     stackcollapse in.tapj stackcollapse.txt

func Main(args []string) error {
	flags := flag.NewFlagSet("stackcollapse", flag.ContinueOnError)

	err := flags.Parse(args)
	if err != nil {
		return err
	}

	input := "-"
	output := "-"

	var writer io.WriteCloser

	switch {
	case len(args) == 1:
		input = args[0]
	case len(args) == 2:
		input = args[0]
		output = args[1]
	}

	if output == "-" {
		writer = os.Stdout
	} else {
		writer, err = os.Create(output)
		if err != nil {
			return err
		}
		defer writer.Close()
	}

	if err := decodeStacktrace(input, os.Stdin, writer); err != nil {
		return err
	}

	return nil
}
