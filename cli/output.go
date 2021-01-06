package cli

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

func ShowTerminalCursor() {
	if !IsStdoutTerminal() {
		return
	}
	// from Go sample at https://rosettacode.org/wiki/Terminal_control/Hiding_the_cursor#Escape_code
	fmt.Print("\033[?25h")
}

type contextKey string

func (c contextKey) String() string {
	return "cli_output context key " + string(c)
}

type OutConfig struct {
	isVerbose       bool
	spinner         *spinner.Spinner
	spinLogBuffer   *spinningLogBuffer
	lastProgress    *int64
}

var cliOutMgrContextKey = contextKey("cliOutMgr")

func Out(ctx context.Context) OutConfig {
	cliOut, ok := ctx.Value(cliOutMgrContextKey).(OutConfig)
	if ok {
		return cliOut
	}
	return OutConfig{}
}

func WithCLIOut(ctx context.Context) context.Context {
	_, ok := ctx.Value(cliOutMgrContextKey).(OutConfig)
	if ok {
		return ctx
	}
	return context.WithValue(ctx, cliOutMgrContextKey, OutConfig{})
}

func WithVerboseOut(ctx context.Context) context.Context {
	cliOut, ok := ctx.Value(cliOutMgrContextKey).(OutConfig)
	if !ok {
		cliOut = OutConfig{}
	}
	cliOut.isVerbose = true
	return context.WithValue(ctx, cliOutMgrContextKey, cliOut)
}

func initSpinner(ctx context.Context) (context.Context, context.CancelFunc) {
	cliOut, ok := ctx.Value(cliOutMgrContextKey).(OutConfig)
	if !ok {
		cliOut = OutConfig{}
	}
	if cliOut.lastProgress == nil {
		p := int64(0)
		cliOut.lastProgress = &p
	}

	if cliOut.isVerbose || !IsStdoutTerminal() {
		return context.WithCancel(context.WithValue(ctx, cliOutMgrContextKey, cliOut))
	}

	if cliOut.spinner == nil {
		cliOut.spinner = spinner.New(spinner.CharSets[14], 50*time.Millisecond)
	}
	if cliOut.spinLogBuffer == nil {
		cliOut.spinLogBuffer = &spinningLogBuffer{}
	}

	_ = cliOut.spinner.Color("reset")
	cliOut.spinner.HideCursor = true
	if !cliOut.spinner.Active() {
		cliOut.spinner.Start()
	}

	ctx, cancel := context.WithCancel(ctx)

	go func() {
		<- ctx.Done()
		cliOut.spinner.Stop()
		ShowTerminalCursor()
		if cliOut.spinLogBuffer != nil && len(cliOut.spinLogBuffer.logs) > 0 {
			cliOut.LogMulti(cliOut.spinLogBuffer.logs)
			cliOut.spinLogBuffer.logs = nil
		}
	}()

	return context.WithValue(ctx, cliOutMgrContextKey, cliOut), cancel
}

func WithSpinner(ctx context.Context, initialMsg string) (context.Context, func(string), context.CancelFunc) {
	ctx, cancel := initSpinner(ctx)
	cliOut, ok := ctx.Value(cliOutMgrContextKey).(OutConfig)
	if !ok {
		panic("initSpinner must set cliOutMgrContextKey")
	}
	if cliOut.spinner == nil {
		if cliOut.isVerbose {
			return ctx, func(s string) {
				cliOut.Verbose(s)
			}, cancel
		} else {
			// not verbose, but standard out is noninteractive, so do nothing:
			return ctx, func(s string) {}, cancel
		}
	}

	update := func(msg string) {
		maxWidth, _, err := term.GetSize(int(os.Stdout.Fd()))
		if err != nil || maxWidth == 0 {
			maxWidth = int(math.Round(80.0 * 0.75))
		} else {
			maxWidth = int(math.Round(float64(maxWidth) * 0.75))
		}

		suffix := " " + msg
		if len(suffix) > maxWidth {
			suffix = suffix[:maxWidth-3] + "..."
		}
		if cliOut.spinner != nil {
			cliOut.spinner.Suffix = suffix
		}
	}
	update(initialMsg)

	return ctx, update, cancel
}

func WithProgress(ctx context.Context, verb string, progressTotal int64) (context.Context, func(int64), context.CancelFunc) {
	ctx, cancel := initSpinner(ctx)
	cliOut, ok := ctx.Value(cliOutMgrContextKey).(OutConfig)
	if !ok {
		panic("initSpinner must set cliOutMgrContextKey")
	}
	if cliOut.spinner == nil {
		if cliOut.isVerbose {
			return ctx, func(progress int64) {
				oldProgress := 10*float64(*cliOut.lastProgress)/float64(progressTotal)
				newProgress := 10*float64(progress)/float64(progressTotal)
				if math.Abs(math.Floor(newProgress) - math.Floor(oldProgress)) > 0.01 {
					cliOut.Verbose(fmt.Sprintf("%s %d / %d (%.f%%)", verb, progress, progressTotal, math.Round(10*newProgress)))
				}
				*cliOut.lastProgress = progress
			}, cancel
		} else {
			return ctx, func(progress int64) {
				oldProgress := float64(*cliOut.lastProgress)/float64(progressTotal)
				newProgress := float64(progress)/float64(progressTotal)
				if (oldProgress < 0.25 && newProgress >= 0.25) || (oldProgress < 0.5 && newProgress >= 0.5) || (oldProgress < 0.75 && newProgress >= 0.75) || (oldProgress < 1.0 && newProgress >= 1.0) {
					cliOut.Log(fmt.Sprintf("%s %d / %d (%.f%%)", verb, progress, progressTotal, math.Round(100*newProgress)))
				}
				*cliOut.lastProgress = progress
			}, cancel
		}
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

func (c OutConfig) HasSpinner() bool {
	return c.spinner != nil
}

func (c OutConfig) Warning(msg string) {
	c.Log("[warning] " + msg)
}

func (c OutConfig) Warnings(msgs []string) {
	for _, msg := range msgs {
		c.Warning(msg)
	}
}

func (c OutConfig) Log(msg string) {
	if c.isVerbose && EchoLogsToStdErr() {
		c.Verbose(msg)
	}
	if c.spinner != nil && c.spinner.Active() && c.spinLogBuffer != nil {
		c.spinLogBuffer.logs = append(c.spinLogBuffer.logs, msg)
	} else {
		fmt.Println(msg)
	}
}

func (c OutConfig) LogMulti(msgs []string) {
	for _, msg := range msgs {
		c.Log(msg)
	}
}

func (c OutConfig) Verbose(msg string) {
	if !c.isVerbose {
		return
	}
	log.Println(msg)
}

func (c OutConfig) VerboseMulti(msgs []string) {
	if !c.isVerbose {
		return
	}
	for _, msg := range msgs {
		c.Verbose(msg)
	}
}
