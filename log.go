// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package log implements a simple logging package. It defines a type, Logger,
// with methods for formatting output. It also has a predefined 'standard'
// Logger accessible through helper functions Print[f|ln], Fatal[f|ln], and
// Panic[f|ln], which are easier to use than creating a Logger manually.
// That logger writes to standard error and prints the date and time
// of each logged message.
// The Fatal functions call os.Exit(1) after writing the log message.
// The Panic functions call panic after writing the log message.
package golog

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
	"time"
)

// These flags define which text to prefix to each log entry generated by the Logger.
const (
	// Bits or'ed together to control what's printed. There is no control over the
	// order they appear (the order listed here) or the format they present (as
	// described in the comments).  A colon appears after these items:
	//	2009/01/23 01:23:23.123123 /a/b/c/d.go:23: message
	Ldate         = 1 << iota     // the date: 2009/01/23
	Ltime                         // the time: 01:23:23
	Lmicroseconds                 // microsecond resolution: 01:23:23.123123.  assumes Ltime.
	Llongfile                     // full file name and line number: /a/b/c/d.go:23
	Lshortfile                    // final file name element and line number: d.go:23. overrides Llongfile
	LstdFlags     = Ldate | Ltime // initial values for the standard logger

)

// A Logger represents an active logging object that generates lines of
// output to an io.Writer.  Each logging operation makes a single call to
// the Writer's Write method.  A Logger can be used simultaneously from
// multiple goroutines; it guarantees to serialize access to the Writer.
type Logger struct {
	mu          sync.Mutex // ensures atomic writes; protects the following fields
	flag        int        // properties
	buf         []byte     // for accumulating text to write
	level       Level
	panicLevel  Level
	enableColor bool
	name        string
	colorFile   *ColorFile

	fileOutput *os.File
}

// New creates a new Logger.   The out variable sets the
// destination to which log data will be written.
// The prefix appears at the beginning of each generated log line.
// The flag argument defines the logging properties.

func New(name string) *Logger {
	l := &Logger{flag: LstdFlags, level: Level_Debug, name: name, panicLevel: Level_Fatal}

	add(l)

	return l
}

func (self *Logger) SetFlag(v int) {
	self.flag = v
}

func (self *Logger) Flag() int {
	return self.flag
}

func (self *Logger) Name() string {
	return self.name
}

// Cheap integer to fixed-width decimal ASCII.  Give a negative width to avoid zero-padding.
// Knows the buffer has capacity.
func itoa(buf *[]byte, i int, wid int) {
	var u uint = uint(i)
	if u == 0 && wid <= 1 {
		*buf = append(*buf, '0')
		return
	}

	// Assemble decimal in reverse order.
	var b [32]byte
	bp := len(b)
	for ; u > 0 || wid > 0; u /= 10 {
		bp--
		wid--
		b[bp] = byte(u%10) + '0'
	}
	*buf = append(*buf, b[bp:]...)
}

func (self *Logger) formatHeader(buf *[]byte, t time.Time, file string, line int) {

	*buf = append(*buf, ' ')
	if self.flag&(Ldate|Ltime|Lmicroseconds) != 0 {
		if self.flag&Ldate != 0 {
			year, month, day := t.Date()
			itoa(buf, year, 4)
			*buf = append(*buf, '/')
			itoa(buf, int(month), 2)
			*buf = append(*buf, '/')
			itoa(buf, day, 2)
			*buf = append(*buf, ' ')
		}
		if self.flag&(Ltime|Lmicroseconds) != 0 {
			hour, min, sec := t.Clock()
			itoa(buf, hour, 2)
			*buf = append(*buf, ':')
			itoa(buf, min, 2)
			*buf = append(*buf, ':')
			itoa(buf, sec, 2)
			if self.flag&Lmicroseconds != 0 {
				*buf = append(*buf, '.')
				itoa(buf, t.Nanosecond()/1e3, 6)
			}
			*buf = append(*buf, ' ')
		}
	}
	if self.flag&(Lshortfile|Llongfile) != 0 {
		if self.flag&Lshortfile != 0 {
			short := file
			for i := len(file) - 1; i > 0; i-- {
				if file[i] == '/' {
					short = file[i+1:]
					break
				}
			}
			file = short
		}
		*buf = append(*buf, file...)
		*buf = append(*buf, ':')
		itoa(buf, line, -1)
		*buf = append(*buf, ": "...)
	}
}

// Output writes the output for a logging event.  The string s contains
// the text to print after the prefix specified by the flags of the
// Logger.  A newline is appended if the last character of s is not
// already a newline.  Calldepth is used to recover the PC and is
// provided for generality, although at the moment on all pre-defined
// paths it will be 2.
func (self *Logger) Output(calldepth int, prefix string, text string, c Color, out io.Writer) error {
	now := time.Now() // get this early.
	var file string
	var line int
	self.mu.Lock()
	defer self.mu.Unlock()
	if self.flag&(Lshortfile|Llongfile) != 0 {
		// release lock while getting caller info - it'text expensive.
		self.mu.Unlock()
		var ok bool
		_, file, line, ok = runtime.Caller(calldepth)
		if !ok {
			file = "???"
			line = 0
		}
		self.mu.Lock()
	}
	self.buf = self.buf[:0]

	colorLog := c != NoColor

	if colorLog {
		self.buf = append(self.buf, logColorPrefix[c]...)
	}

	self.buf = append(self.buf, prefix...)
	self.formatHeader(&self.buf, now, file, line)
	self.buf = append(self.buf, text...)

	if colorLog {
		self.buf = append(self.buf, logColorSuffix...)
	}

	if len(text) > 0 && text[len(text)-1] != '\n' {
		self.buf = append(self.buf, '\n')
	}

	_, err := out.Write(self.buf)

	return err
}

func (self *Logger) Log(c Color, level Level, format string, v ...interface{}) {

	if level < self.level {
		return
	}

	prefix := fmt.Sprintf("%s %s", levelString[level], self.name)

	var text string

	if format == "" {
		text = fmt.Sprintln(v...)
	} else {
		text = fmt.Sprintf(format, v...)
	}

	var out io.Writer

	if self.enableColor {

		if self.colorFile != nil && c == NoColor {
			c = self.colorFile.ColorFromText(text)
		}

		if level >= Level_Error {
			c = Red
		}
	} else {
		c = NoColor
	}

	if self.fileOutput == nil {
		out = os.Stdout
	} else {
		out = self.fileOutput
	}

	self.Output(3, prefix, text, c, out)

	if int(level) >= int(self.panicLevel) {
		panic(text)
	}

}

func (self *Logger) DebugColorf(colorName string, format string, v ...interface{}) {

	if c, ok := colorByName[colorName]; ok {
		self.Log(c, Level_Debug, format, v...)
	} else {
		self.Log(White, Level_Debug, format, v...)
	}

}

func (self *Logger) DebugColorln(colorName string, v ...interface{}) {

	if c, ok := colorByName[colorName]; ok {
		self.Log(c, Level_Debug, "", v...)
	} else {
		self.Log(White, Level_Debug, "", v...)
	}
}

func (self *Logger) Debugf(format string, v ...interface{}) {

	self.Log(ColorFromLevel(Level_Debug), Level_Debug, format, v...)
}

func (self *Logger) Debugln(v ...interface{}) {
	self.Log(ColorFromLevel(Level_Debug), Level_Debug, "", v...)
}

func (self *Logger) Infof(format string, v ...interface{}) {

	self.Log(ColorFromLevel(Level_Info), Level_Info, format, v...)
}

func (self *Logger) Infoln(v ...interface{}) {
	self.Log(ColorFromLevel(Level_Info), Level_Info, "", v...)
}

func (self *Logger) Warnf(format string, v ...interface{}) {

	self.Log(ColorFromLevel(Level_Warn), Level_Warn, format, v...)
}

func (self *Logger) Warnln(v ...interface{}) {
	self.Log(ColorFromLevel(Level_Warn), Level_Warn, "", v...)
}

func (self *Logger) Errorf(format string, v ...interface{}) {

	self.Log(ColorFromLevel(Level_Error), Level_Error, format, v...)
}

func (self *Logger) Errorln(v ...interface{}) {
	self.Log(ColorFromLevel(Level_Error), Level_Error, "", v...)
}

func (self *Logger) Fatalf(format string, v ...interface{}) {

	self.Log(ColorFromLevel(Level_Fatal), Level_Fatal, format, v...)
}

func (self *Logger) Fatalln(v ...interface{}) {
	self.Log(ColorFromLevel(Level_Fatal), Level_Fatal, "", v...)
}

func (self *Logger) SetLevelByString(level string) {

	self.SetLevel(str2loglevel(level))

}

func (self *Logger) SetLevel(lv Level) {
	self.level = lv
}

func (self *Logger) Level() Level {
	return self.level
}

func (self *Logger) SetPanicLevelByString(level string) {
	self.panicLevel = str2loglevel(level)

}

// 注意, 加色只能在Gogland的main方式启用, Test方式无法加色
func (self *Logger) SetColorFile(file *ColorFile) {
	self.colorFile = file
}
func (self *Logger) IsDebugEnabled() bool {
	return self.level == Level_Debug
}
