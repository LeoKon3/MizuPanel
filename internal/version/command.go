package version

import (
	"fmt"
	"io"
)

// PrintCommand writes the component version for the supported standalone
// version arguments. It returns false when normal command startup should continue.
func PrintCommand(args []string, component string, output io.Writer) (bool, error) {
	if len(args) != 1 {
		return false, nil
	}
	switch args[0] {
	case "version", "--version", "-v":
		_, err := fmt.Fprintf(output, "MizuPanel %s v%s\n", component, Current)
		return true, err
	default:
		return false, nil
	}
}
