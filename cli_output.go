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

func EchoLogsToStdErr() bool {
	return (IsStdoutTerminal() != IsStderrTerminal()) || (!IsStdoutTerminal() && !IsStderrTerminal())
}

type contextKey string

func (c contextKey) String() string {
	return "cli_output context key " + string(c)
}

type CLIOutConfig struct {
	verbose      bool
	spinner      *spinner.Spinner
	spinnerTotal int64
	maxWidth     int
}

var cliOutMgrContextKey = contextKey("cliOutMgr")

func (c *CLIOutConfig) useProgressIndicators() bool {
	return !c.verbose && IsStdoutTerminal()
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
	cliOut.verbose = true
	return context.WithValue(ctx, cliOutMgrContextKey, cliOut)
}

func WithSpinner(ctx context.Context) (context.Context, context.CancelFunc) {
	cliOut, ok := ctx.Value(cliOutMgrContextKey).(CLIOutConfig)
	if !ok {
		cliOut = CLIOutConfig{}
	}

	// TODO(cdzombak): update periodically (200ms) if spinner is active, but be sure not to leak goroutines doing that forever eg. take cancellation into account
	maxWidth, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || maxWidth == 0 {
		maxWidth = int(math.Round(80.0 * 0.7))
	} else {
		maxWidth = int(math.Round(float64(maxWidth) * 0.75))
	}
	cliOut.maxWidth = maxWidth

	if cliOut.spinner == nil {
		cliOut.spinner = spinner.New(spinner.CharSets[14], 50*time.Millisecond)
	}
	_ = cliOut.spinner.Color("reset")
	cliOut.spinner.HideCursor = true
	if !cliOut.spinner.Active() {
		cliOut.spinner.Start()
	}

	ctx, cancel := context.WithCancel(ctx)
	cancel2 := func() {
		cliOut.spinner.HideCursor = false
		cliOut.spinner.Stop()
		cancel()
	}

	return context.WithValue(ctx, cliOutMgrContextKey, cliOut), cancel2
}

func SpinnerTotal(ctx context.Context, total int64) context.Context {
	cliOut, ok := ctx.Value(cliOutMgrContextKey).(CLIOutConfig)
	if !ok {
		cliOut = CLIOutConfig{}
	}
	cliOut.spinnerTotal = total
	return context.WithValue(ctx, cliOutMgrContextKey, cliOut)
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
	if c.verbose && EchoLogsToStdErr() {
		c.Verbose(msg)
	}
	// TODO(cdzombak): should buffer these until cancel/spinner is done iff spinner is active
	fmt.Println(msg)
}

func (c CLIOutConfig) LogMulti(msgs []string) {
	for _, msg := range msgs {
		c.Log(msg)
	}
}

func (c CLIOutConfig) Verbose(msg string) {
	if !c.verbose {
		return
	}
	log.Println(msg)
}

func (c CLIOutConfig) VerboseMulti(msgs []string) {
	if !c.verbose {
		return
	}
	for _, msg := range msgs {
		c.Verbose(msg)
	}
}

func (c CLIOutConfig) SpinMessage(msg string) {
	suffix := " " + msg
	if len(suffix) > c.maxWidth {
		suffix = suffix[:c.maxWidth-3] + "..."
	}
	if c.spinner != nil {
		c.spinner.Suffix = suffix
	}
}

func (c CLIOutConfig) SpinProgress(n int64, verb string) {
	if c.spinner == nil {
		return
	}
	if len(verb) > 0 {
		verb = " " + verb
	}
	if c.spinnerTotal > 0 {
		c.spinner.Suffix = fmt.Sprintf("%s %d / %d (%.f%%)", verb, n, c.spinnerTotal, math.Round(100*float64(n)/float64(c.spinnerTotal)))
	} else {
		c.spinner.Suffix = fmt.Sprintf("%s #%d ...", verb, n)
	}
}
