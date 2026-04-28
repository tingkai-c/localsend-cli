package clipboard

import (
	clipboard "github.com/atotto/clipboard"
	"github.com/tingkai-c/localsend-tui/internal/utils/logger"
)

func WriteToClipBoard(text string) {
	err := clipboard.WriteAll(text)
	if err != nil {
		logger.Errorf("Error copying to clipboard: %v\n", err)
	} else {
		logger.Success("Text copied to clipboard!")
	}
}
