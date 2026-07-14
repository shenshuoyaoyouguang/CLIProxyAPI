package helps

import (
	"bufio"
	"context"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	log "github.com/sirupsen/logrus"
)

// SSEPumpSpec configures a bufio-scanner streaming pump. It captures the
// mechanical scaffolding shared by the HTTP SSE executors (gemini, kimi,
// vertex, antigravity, codex, xai) while leaving per-line and terminal
// semantics to caller-supplied closures.
//
// Byte-equivalence contract: the pump reproduces the exact scan/translate/send
// ordering of the hand-written loops it replaces. ProcessLine and Finalize own
// all payload transformation and usage publication; the pump owns the scanner,
// buffer sizing, raw-line response logging, channel plumbing, context
// cancellation, response-body close, and terminal scanner-error handling.
type SSEPumpSpec struct {
	// Cfg is used for raw response-chunk and error logging. May be nil.
	Cfg *config.Config
	// Reporter receives failure observations on a terminal scanner error.
	// May be nil.
	Reporter *UsageReporter
	// BufferSize sets the scanner's maximum token size. Zero keeps bufio's
	// default buffer.
	BufferSize int
	// Provider labels the response-body close error log. Empty falls back to a
	// generic label.
	Provider string
	// ProcessLine transforms one raw scanned line into zero or more downstream
	// chunk payloads. It may publish usage via the reporter. Returning nil skips
	// the line.
	ProcessLine func(line []byte) [][]byte
	// Finalize runs once after a successful scan loop (before the scanner-error
	// check) and returns trailing chunk payloads, e.g. the translated [DONE]
	// terminator. May be nil.
	Finalize func() [][]byte
	// OnExit runs as the goroutine's first deferred action (before the response
	// body is closed and before the channel is closed), regardless of success or
	// scanner error. It mirrors the per-executor "flush buffered usage at end"
	// defers (e.g. StreamUsageBuffer.Publish). May be nil.
	OnExit func()
	// OnScanSuccess runs after Finalize only when the scan loop ended without a
	// scanner error. It mirrors the per-executor "else { reporter.EnsurePublished }"
	// success branch. May be nil.
	OnScanSuccess func()
}

// PumpSSEStream drains an upstream SSE response body on a background goroutine,
// forwarding chunks produced by spec.ProcessLine (and spec.Finalize) over the
// returned StreamResult channel. It replaces the duplicated per-executor
// scanner loops without altering their observable output.
//
// The caller must not read resp.Body after this call; the pump owns it and
// closes it when the goroutine exits.
func PumpSSEStream(ctx context.Context, resp *http.Response, spec SSEPumpSpec) *cliproxyexecutor.StreamResult {
	out := make(chan cliproxyexecutor.StreamChunk)
	go func() {
		defer close(out)
		defer func() {
			if errClose := resp.Body.Close(); errClose != nil {
				provider := spec.Provider
				if provider == "" {
					provider = "sse pump"
				}
				log.Errorf("%s: close response body error: %v", provider, errClose)
			}
		}()
		// Registered after the body-close defer so LIFO order runs OnExit first,
		// matching the per-executor "flush usage before body/channel close" defers.
		if spec.OnExit != nil {
			defer spec.OnExit()
		}

		scanner := bufio.NewScanner(resp.Body)
		if spec.BufferSize > 0 {
			scanner.Buffer(nil, spec.BufferSize)
		}
		for scanner.Scan() {
			line := scanner.Bytes()
			AppendAPIResponseChunk(ctx, spec.Cfg, line)
			if spec.ProcessLine == nil {
				continue
			}
			chunks := spec.ProcessLine(line)
			for i := range chunks {
				select {
				case out <- cliproxyexecutor.StreamChunk{Payload: chunks[i]}:
				case <-ctx.Done():
					return
				}
			}
		}
		if spec.Finalize != nil {
			for _, chunk := range spec.Finalize() {
				select {
				case out <- cliproxyexecutor.StreamChunk{Payload: chunk}:
				case <-ctx.Done():
					return
				}
			}
		}
		if errScan := scanner.Err(); errScan != nil {
			RecordAPIResponseError(ctx, spec.Cfg, errScan)
			if spec.Reporter != nil {
				spec.Reporter.PublishFailure(ctx, errScan)
			}
			select {
			case out <- cliproxyexecutor.StreamChunk{Err: errScan}:
			case <-ctx.Done():
			}
		} else if spec.OnScanSuccess != nil {
			spec.OnScanSuccess()
		}
	}()
	return &cliproxyexecutor.StreamResult{Headers: resp.Header.Clone(), Chunks: out}
}
