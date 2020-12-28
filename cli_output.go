package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"time"

	"github.com/briandowns/spinner"
	"golang.org/x/term"
)

// IsStdoutTerminal returns true iff standard output is an interactive terminal.
func IsStdoutTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// IsStderrTerminal returns true iff standard err is an interactive terminal.
func IsStderrTerminal() bool {
	return term.IsTerminal(int(os.Stderr.Fd()))
}

// EchoLogsToStdErr returns true iff messages sent to standard out should
// also be echoed to standard error.
func EchoLogsToStdErr() bool {
	return (IsStdoutTerminal() != IsStderrTerminal()) || (!IsStdoutTerminal() && !IsStderrTerminal())
}

type contextKey string

func (c contextKey) String() string {
	return "cli_output context key " + string(c)
}

type CLIOutConfig struct {
	isVerbose       bool
	spinner         *spinner.Spinner
	maxSpinMsgWidth int
	spinLogBuffer   *spinningLogBuffer
}

var cliOutMgrContextKey = contextKey("cliOutMgr")

func (c *CLIOutConfig) useProgressIndicators() bool {
	return !c.isVerbose && IsStdoutTerminal()
}

func CLIOut(ctx context.Context) CLIOutConfig {
	cliOut, ok := ctx.Value(cliOutMgrContextKey).(CLIOutConfig)
	if ok {
		return cliOut
	}
	return CLIOutConfig{}
}

func WithCLIOut(ctx context.Context) context.Context {
	_, ok := ctx.Value(cliOutMgrContextKey).(CLIOutConfig)
	if ok {
		return ctx
	}
	return context.WithValue(ctx, cliOutMgrContextKey, CLIOutConfig{})
}

func WithVerboseCLIOut(ctx context.Context) context.Context {
	cliOut, ok := ctx.Value(cliOutMgrContextKey).(CLIOutConfig)
	if !ok {
		cliOut = CLIOutConfig{}
	}
	cliOut.isVerbose = true
	return context.WithValue(ctx, cliOutMgrContextKey, cliOut)
}

func initCLISpinner(ctx context.Context) (context.Context, context.CancelFunc) {
	cliOut, ok := ctx.Value(cliOutMgrContextKey).(CLIOutConfig)
	if !ok {
		cliOut = CLIOutConfig{}
	}

	if cliOut.spinner == nil {
		cliOut.spinner = spinner.New(spinner.CharSets[14], 50*time.Millisecond)
	}
	if cliOut.spinLogBuffer == nil {
		cliOut.spinLogBuffer = &spinningLogBuffer{}
	}

	// TODO(cdzombak): update periodically (200ms) if spinner is active, but be sure not to leak goroutines doing that forever eg. take cancellation into account
	maxWidth, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || maxWidth == 0 {
		maxWidth = int(math.Round(80.0 * 0.7))
	} else {
		maxWidth = int(math.Round(float64(maxWidth) * 0.75))
	}
	cliOut.maxSpinMsgWidth = maxWidth

	_ = cliOut.spinner.Color("reset")
	cliOut.spinner.HideCursor = true
	if !cliOut.spinner.Active() {
		cliOut.spinner.Start()
	}

	ctx, cancel := context.WithCancel(ctx)
	cancel2 := func() {
		cliOut.spinner.HideCursor = false
		cliOut.spinner.Stop()
		if cliOut.spinLogBuffer != nil && len(cliOut.spinLogBuffer.logs) > 0 {
			cliOut.LogMulti(cliOut.spinLogBuffer.logs)
		}
		cancel()
	}
	return context.WithValue(ctx, cliOutMgrContextKey, cliOut), cancel2
}

func WithCLISpinner(ctx context.Context, initialMsg string) (context.Context, func(string), context.CancelFunc) {
	ctx, cancel := initCLISpinner(ctx)
	cliOut, ok := ctx.Value(cliOutMgrContextKey).(CLIOutConfig)
	if !ok {
		panic("initCLISpinner must set cliOutMgrContextKey")
	}
	if cliOut.spinner == nil {
		panic("initCLISpinner must set spinner")
	}

	update := func(msg string) {
		suffix := " " + msg
		if len(suffix) > cliOut.maxSpinMsgWidth {
			suffix = suffix[:cliOut.maxSpinMsgWidth-3] + "..."
		}
		if cliOut.spinner != nil {
			cliOut.spinner.Suffix = suffix
		}
	}
	update(initialMsg)

	return ctx, update, cancel
}

func WithCLIProgress(ctx context.Context, verb string, progressTotal int64) (context.Context, func(int64), context.CancelFunc) {
	ctx, cancel := initCLISpinner(ctx)
	cliOut, ok := ctx.Value(cliOutMgrContextKey).(CLIOutConfig)
	if !ok {
		panic("initCLISpinner must set cliOutMgrContextKey")
	}
	if cliOut.spinner == nil {
		panic("initCLISpinner must set spinner")
	}

	update := func(progress int64) {
		if len(verb) > 0 {
			verb = " " + verb
		}
		if progressTotal > 0 {
			cliOut.spinner.Suffix = fmt.Sprintf("%s %d / %d (%.f%%)", verb, progress, progressTotal, math.Round(100*float64(progress)/float64(progressTotal)))
		} else {
			cliOut.spinner.Suffix = fmt.Sprintf("%s #%d ...", verb, progress)
		}
	}
	update(0)

	return ctx, update, cancel
}

type spinningLogBuffer struct {
	logs []string
}

func (c CLIOutConfig) HasSpinner() bool {
	return c.spinner != nil
}

func (c CLIOutConfig) Warning(msg string) {
	c.Log("[warning] " + msg)
}

func (c CLIOutConfig) Warnings(msgs []string) {
	for _, msg := range msgs {
		c.Warning(msg)
	}
}

func (c CLIOutConfig) Log(msg string) {
	if c.isVerbose && EchoLogsToStdErr() {
		c.Verbose(msg)
	}
	if c.spinner != nil && c.spinner.Active() && c.spinLogBuffer != nil {
		c.spinLogBuffer.logs = append(c.spinLogBuffer.logs, msg)
	} else {
		fmt.Println(msg)
	}
}

func (c CLIOutConfig) LogMulti(msgs []string) {
	for _, msg := range msgs {
		c.Log(msg)
	}
}

func (c CLIOutConfig) Verbose(msg string) {
	if !c.isVerbose {
		return
	}
	log.Println(msg)
}

func (c CLIOutConfig) VerboseMulti(msgs []string) {
	if !c.isVerbose {
		return
	}
	for _, msg := range msgs {
		c.Verbose(msg)
	}
}
