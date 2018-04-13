// tracelog.go
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"go.uber.org/multierr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type tracecore struct {
	zapcore.LevelEnabler
	en     zapcore.Encoder
	out    io.Writer
	names  []string
	filter []*regexp.Regexp
}

func (c *tracecore) With(fields []zapcore.Field) zapcore.Core {

	return c
}

func (c *tracecore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}

func (c *tracecore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	buf, err := c.en.EncodeEntry(ent, fields)
	if err != nil {
		return err
	}
	_, err = c.out.Write(buf.Bytes())

	buf.Free()
	if err != nil {
		return err
	}
	if ent.Level > zapcore.ErrorLevel {
		// Since we may be crashing the program, sync the output. Ignore Sync
		// errors, pending a clean solution to issue #370.
		c.Sync()
	}
	return nil
}

func (c *tracecore) Sync() error {
	return nil
}

func (c *tracecore) Filter(params ...string) bool {

	for i, v := range params {
		if i < len(c.filter) {
			if c.filter[i] == nil {
				continue
			}
			if !c.filter[i].MatchString(v) {
				return false
			}
		}
	}
	return true
}

type traceLog struct {
	cores []*tracecore
	enc   zapcore.Encoder
	sync.RWMutex
}

func (l *traceLog) GetLoger(params ...string) *zap.Logger {
	var cores []zapcore.Core
	for _, v := range l.cores {
		if v.Filter(params...) {
			cores = append(cores, zapcore.NewCore(v.en.Clone(), zapcore.AddSync(v.out), v.LevelEnabler))
		}
	}

	return zap.New(zapcore.NewTee(cores...), zap.AddCaller())
}

type iowriterFun func([]byte) (int, error)

func (f iowriterFun) Write(b []byte) (n int, err error) {
	n, err = f(b)
	return
}

func (c *tracecore) MakeFilter(filters ...zapcore.Field) {
	for _, v := range filters {
		reg, _ := regexp.Compile(v.String)
		c.names = append(c.names, v.Key)
		c.filter = append(c.filter, reg)
	}
}

func (c *tracecore) FilterCheck(fields ...zapcore.Field) bool {
	for i, v := range c.names {
		bmatch := false
		for _, fv := range fields {
			if fv.Key == v {
				if c.filter[i].MatchString(fv.String) == false {
					return false
				}
				bmatch = true
			}
		}
		if bmatch == false {
			return false
		}
	}
	return true
}

func (l *traceLog) AddTrace(patterns []string, out io.Writer, level int) *tracecore {

	ptrace := &tracecore{
		en:           l.enc,
		LevelEnabler: zapcore.Level(level),
		out:          out,
	}

	for _, v := range patterns {

		reg, _ := regexp.Compile(v)

		ptrace.filter = append(ptrace.filter, reg)
	}

	l.Lock()

	l.cores = append(l.cores, ptrace)

	l.Unlock()
	//f,err:=os.OpenFile("sss",0,0)
	//f.Write(nil)
	//f.Sync()
	return ptrace
}

func (l *traceLog) RemoveTrace(c *tracecore) error {
	index := -1
	l.Lock()
	defer l.Unlock()
	for k, v := range l.cores {
		if v == c {
			index = k
			break
		}
	}

	if index >= 0 {
		l.cores = append(l.cores[:index], l.cores[index+1:]...)
		return nil
	}

	return fmt.Errorf("no found")
}

func NewTrace() *traceLog {

	enc := zapcore.NewJSONEncoder(zapcore.EncoderConfig{
		// Keys can be anything except the empty string.
		TimeKey:        "T",
		LevelKey:       "L",
		NameKey:        "N",
		CallerKey:      "C",
		MessageKey:     "M",
		StacktraceKey:  "S",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalLevelEncoder,
		EncodeTime:     zapcore.EpochTimeEncoder, //ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	})

	return &traceLog{
		enc: enc,
	}

}

var (
	_logfile *os.File = nil
	pid               = os.Getpid()
	program           = filepath.Base(os.Args[0])
)

func GetLogFile() *os.File {
	if _logfile == nil {

		name := fmt.Sprintf("%s-%d-%s.log", program, pid, time.Now().Format("2006-0102-030406"))

		_logfile, _ = os.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0666)

	}

	return _logfile
}

type multicore struct {
	cores []zapcore.Core
	lock  sync.Mutex
}

func (mc *multicore) AddCoreIO(io io.Writer, l zapcore.Level) zapcore.Core {
	core := zapcore.NewCore(encjson.Clone(), zapcore.AddSync(io), l)
	mc.AddCore(core)
	return core
}

func (mc *multicore) AddCore(c zapcore.Core) {
	mc.lock.Lock()
	mc.cores = append(mc.cores, c)
	mc.lock.Unlock()
}

func (mc *multicore) DelCore(c zapcore.Core) {
	mc.lock.Lock()
	for i := range mc.cores {
		if c == mc.cores[i] {
			mc.cores = append(mc.cores[:i], mc.cores[i+1:]...)
		}
	}
	mc.lock.Unlock()
}

func (c *multicore) Enabled(l zapcore.Level) bool {
	for _, c := range c.cores {
		if c.Enabled(l) {
			return true
		}
	}
	return false
}

func (c *multicore) With(fields []zapcore.Field) zapcore.Core {

	return c
}

func (cores *multicore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {

	for _, c := range cores.cores {
		if c.Enabled(ent.Level) {
			ce = ce.AddCore(ent, c)
		}
	}
	return ce
}

func (c *multicore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	var err error
	fmt.Println("start write")
	for i := range c.cores {
		err = multierr.Append(err, c.cores[i].Write(ent, fields))
	}
	return err
}

func (c *multicore) Sync() error {
	var err error
	for i := range c.cores {
		err = multierr.Append(err, c.cores[i].Sync())
	}
	return err
}

func NewLogger(options ...zap.Option) (*zap.SugaredLogger, *multicore) {
	core := &multicore{}

	log := zap.New(core, options...)
	return log.Sugar(), core
}

var encjson = zapcore.NewJSONEncoder(zapcore.EncoderConfig{
	// Keys can be anything except the empty string.
	TimeKey:        "T",
	LevelKey:       "L",
	NameKey:        "N",
	CallerKey:      "C",
	MessageKey:     "M",
	StacktraceKey:  "S",
	LineEnding:     zapcore.DefaultLineEnding,
	EncodeLevel:    zapcore.CapitalLevelEncoder,
	EncodeTime:     zapcore.EpochTimeEncoder, //ISO8601TimeEncoder,
	EncodeDuration: zapcore.StringDurationEncoder,
	EncodeCaller:   zapcore.ShortCallerEncoder,
})
