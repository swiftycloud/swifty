package main

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"context"
	"errors"
)

var glog *zap.SugaredLogger

func l2z(v string) (zapcore.Level, error) {
	switch v {
	case "debug":
		return zap.DebugLevel, nil
	case "info":
		return zap.InfoLevel, nil
	case "warn":
		return zap.WarnLevel, nil
	case "error":
		return zap.ErrorLevel, nil
	default:
		return zap.WarnLevel, errors.New("Unknown level")
	}
}

func z2l(l zapcore.Level) string {
	switch l {
	case zap.DebugLevel:
		return "debug"
	case zap.InfoLevel:
		return "info"
	case zap.WarnLevel:
		return "warn"
	case zap.ErrorLevel:
		return "error"
	default:
		return "?"
	}
}

func setupLogger(conf *YAMLConf) {
	l, _ := l2z(conf.Daemon.LogLevel)
	lvl := zap.NewAtomicLevelAt(l)

	addSysctl("gate_log_level",
		func() string { return z2l(lvl.Level()) },
		func(v string) error {
			nl, er := l2z(v)
			if er == nil {
				lvl.SetLevel(nl)
			}
			return er
		})

	zcfg := zap.Config {
		Level:            lvl,
		Development:      true,
		DisableStacktrace:true,
		Encoding:         "console",
		EncoderConfig:    zap.NewDevelopmentEncoderConfig(),
		OutputPaths:      []string{"stderr"},
		ErrorOutputPaths: []string{"stderr"},
	}

	logger, _ := zcfg.Build()
	glog = logger.Sugar()
}

func ctxlog(ctx context.Context) *zap.SugaredLogger {
	if gctx, ok := ctx.(*gateContext); ok {
		lfor := gctx.Desc
		if gctx.Tenant != "" {
			lfor += ":" + gctx.Tenant
		}
		return glog.With(zap.Int64("r", int64(gctx.ReqId)), zap.String("f", lfor))
	}

	return glog
}
