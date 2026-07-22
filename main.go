// Command marker-cli converts PDFs to Markdown using MistralAI OCR.
package main

import (
	"os"

	"github.com/l3-n0x/marker-cli/internal/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
