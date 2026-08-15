package sipgw

import (
	"github.com/yuanshuai1122/vodoge/pkg/logger"
)

func shouldLogSIPRaw() bool {
	return logger.ShouldLogSIPRaw()
}

func redactSIPRaw(raw string) string {
	return logger.RedactSIPRaw(raw)
}
