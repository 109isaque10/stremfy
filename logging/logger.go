package logging

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// NewMultiSinkLogger builds a logger configured as:
// - Console (pretty) + Info-file -> accepts Level >= Info
// - Debug-file -> accepts Level >= Debug (enabled only when debugMode == true)
//
// infoPath and debugPath can be absolute or relative paths to the log files.
// debugMode toggles whether the debug file core is attached.
func NewMultiSinkLogger(infoPath, debugPath string, debugMode bool) (*zap.Logger, error) {
	// Encoder configs
	consoleEncCfg := zap.NewDevelopmentEncoderConfig()
	consoleEncCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
	consoleEncCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	consoleEncoder := zapcore.NewConsoleEncoder(consoleEncCfg)

	fileEncCfg := zap.NewProductionEncoderConfig()
	fileEncCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	fileEncoder := zapcore.NewJSONEncoder(fileEncCfg)

	// Lumberjack writers (rotation)
	infoWriter := &lumberjack.Logger{
		Filename:   infoPath,
		MaxSize:    50,   // megabytes
		MaxBackups: 7,    // number of rotated files to keep
		MaxAge:     28,   // days
		Compress:   true, // gzip old files
	}
	// debugWriter may be unused when debugMode == false
	debugWriter := &lumberjack.Logger{
		Filename:   debugPath,
		MaxSize:    100,
		MaxBackups: 14,
		MaxAge:     60,
		Compress:   true,
	}

	// Level enablers:
	infoLevel := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return lvl >= zapcore.InfoLevel
	})
	debugLevel := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return lvl >= zapcore.DebugLevel
	})

	// Build cores:
	cores := make([]zapcore.Core, 0, 3)

	// Console core (pretty) for Info+
	consoleCore := zapcore.NewCore(consoleEncoder, zapcore.Lock(os.Stdout), infoLevel)
	cores = append(cores, consoleCore)

	// Info file core (JSON) for Info+
	infoFileCore := zapcore.NewCore(fileEncoder, zapcore.AddSync(infoWriter), infoLevel)
	cores = append(cores, infoFileCore)

	// Debug file core (JSON) for Debug+
	if debugMode {
		debugFileCore := zapcore.NewCore(fileEncoder, zapcore.AddSync(debugWriter), debugLevel)
		cores = append(cores, debugFileCore)
	}

	// Combine cores
	core := zapcore.NewTee(cores...)

	// Add caller info and stacktrace threshold
	logger := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))

	return logger, nil
}
