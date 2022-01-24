package tfaps

import "strings"

// Sanitize strip newlines from externally-sources strings to avoid log injection attacks
func Sanitize(input string) string {
	output := strings.Replace(input, "\n", "", -1)
	output = strings.Replace(output, "\r", "", -1)
	return output
}
