package facts

import (
	"net/http"
	"testing"
)

func TestFileFacts_BuildsHTTPTransferOperationFacts(t *testing.T) {
	t.Parallel()

	fileFacts := makeFileFacts(t, `FROM alpine:3.20
RUN curl -fsSL -o /tmp/app.tgz https://example.com/app.tgz
RUN wget -qO- https://example.com/index.txt
`)

	stage := fileFacts.Stage(0)
	if stage == nil {
		t.Fatal("expected stage facts")
	}
	if len(stage.Runs) != 2 {
		t.Fatalf("expected 2 RUN facts, got %d", len(stage.Runs))
	}

	curlFact := stage.Runs[0].CommandOperationFacts[0]
	if curlFact.Status != CommandOperationLifted {
		t.Fatalf("curl fact status = %q, want %q", curlFact.Status, CommandOperationLifted)
	}
	if curlFact.Tool != "curl" || curlFact.HTTPTransfer == nil {
		t.Fatalf("unexpected curl fact: %#v", curlFact)
	}
	if curlFact.HTTPTransfer.SinkKind != HTTPTransferSinkFile {
		t.Fatalf("curl sink = %q, want %q", curlFact.HTTPTransfer.SinkKind, HTTPTransferSinkFile)
	}
	if curlFact.HTTPTransfer.OutputPath != "/tmp/app.tgz" {
		t.Fatalf("curl output path = %q, want %q", curlFact.HTTPTransfer.OutputPath, "/tmp/app.tgz")
	}
	if curlFact.SourceRange == nil || curlFact.SourceRange.StartLine != 2 {
		t.Fatalf("curl source range = %#v, want start line 2", curlFact.SourceRange)
	}
	if curlFact.SourceRange.StartCol != len("RUN ") {
		t.Fatalf("curl source start col = %d, want %d", curlFact.SourceRange.StartCol, len("RUN "))
	}
	curlLine := "RUN curl -fsSL -o /tmp/app.tgz https://example.com/app.tgz"
	if curlFact.SourceRange.EndCol != len(curlLine) {
		t.Fatalf("curl source end col = %d, want %d", curlFact.SourceRange.EndCol, len(curlLine))
	}
	if _, ok := curlFact.HTTPTransfer.LowerToTool(httpTransferToolWget); !ok {
		t.Fatal("expected curl -fsSL operation to lower to wget")
	}

	wgetFact := stage.Runs[1].CommandOperationFacts[0]
	if wgetFact.Status != CommandOperationLifted {
		t.Fatalf("wget fact status = %q, want %q", wgetFact.Status, CommandOperationLifted)
	}
	if wgetFact.Tool != "wget" || wgetFact.HTTPTransfer == nil {
		t.Fatalf("unexpected wget fact: %#v", wgetFact)
	}
	if wgetFact.HTTPTransfer.SinkKind != HTTPTransferSinkStdout {
		t.Fatalf("wget sink = %q, want %q", wgetFact.HTTPTransfer.SinkKind, HTTPTransferSinkStdout)
	}
}

func TestFileFacts_BlocksDynamicHTTPTransferOperationFacts(t *testing.T) {
	t.Parallel()

	fileFacts := makeFileFacts(t, `FROM alpine:3.20
RUN curl -fsSL "$URL"
`)

	stage := fileFacts.Stage(0)
	if stage == nil {
		t.Fatal("expected stage facts")
	}
	if len(stage.Runs) != 1 {
		t.Fatalf("expected 1 RUN fact, got %d", len(stage.Runs))
	}
	if len(stage.Runs[0].CommandOperationFacts) != 1 {
		t.Fatalf("expected 1 command operation fact, got %d", len(stage.Runs[0].CommandOperationFacts))
	}

	fact := stage.Runs[0].CommandOperationFacts[0]
	if fact.Status != CommandOperationBlocked {
		t.Fatalf("fact status = %q, want %q", fact.Status, CommandOperationBlocked)
	}
	if fact.HTTPTransfer != nil {
		t.Fatalf("blocked fact should not carry HTTP transfer, got %#v", fact.HTTPTransfer)
	}
	if len(fact.Blockers) == 0 {
		t.Fatal("expected blockers for dynamic curl command")
	}
}

func TestFileFacts_BlocksCompositeDynamicHTTPTransferOperationFacts(t *testing.T) {
	t.Parallel()

	fileFacts := makeFileFacts(t, `FROM alpine:3.20
ARG VERSION=latest
RUN curl -L -o /tmp/app.tgz https://example.com/releases/${VERSION}/app-${VERSION}.tgz
`)

	stage := fileFacts.Stage(0)
	if stage == nil {
		t.Fatal("expected stage facts")
	}
	if len(stage.Runs) != 1 {
		t.Fatalf("expected 1 RUN fact, got %d", len(stage.Runs))
	}
	if len(stage.Runs[0].CommandOperationFacts) != 1 {
		t.Fatalf("expected 1 command operation fact, got %d", len(stage.Runs[0].CommandOperationFacts))
	}

	fact := stage.Runs[0].CommandOperationFacts[0]
	if fact.Status != CommandOperationBlocked {
		t.Fatalf("fact status = %q, want %q", fact.Status, CommandOperationBlocked)
	}
	if fact.HTTPTransfer != nil {
		t.Fatalf("blocked fact should not carry HTTP transfer, got %#v", fact.HTTPTransfer)
	}
}

func TestFileFacts_CurlWithoutFailDoesNotLowerToWget(t *testing.T) {
	t.Parallel()

	fileFacts := makeFileFacts(t, `FROM alpine:3.20
RUN curl -sSL https://example.com/app.tgz
`)

	stage := fileFacts.Stage(0)
	if stage == nil {
		t.Fatal("expected stage facts")
	}
	if len(stage.Runs) != 1 {
		t.Fatalf("expected 1 RUN fact, got %d", len(stage.Runs))
	}
	if len(stage.Runs[0].CommandOperationFacts) != 1 {
		t.Fatalf("expected 1 command operation fact, got %d", len(stage.Runs[0].CommandOperationFacts))
	}

	fact := stage.Runs[0].CommandOperationFacts[0]
	if fact.Status != CommandOperationLifted {
		t.Fatalf("fact status = %q, want %q", fact.Status, CommandOperationLifted)
	}
	if fact.HTTPTransfer == nil {
		t.Fatal("expected lifted HTTP transfer")
	}
	if fact.HTTPTransfer.FailOnHTTPStatus {
		t.Fatal("expected curl -sSL to leave FailOnHTTPStatus unset")
	}
	if _, ok := fact.HTTPTransfer.LowerToTool(httpTransferToolWget); ok {
		t.Fatal("expected curl without -f to refuse wget lowering")
	}
}

func TestHTTPTransferOperation_LowerToCurlRequiresFailAndRedirect(t *testing.T) {
	t.Parallel()

	op := &HTTPTransferOperation{
		URL:              "https://example.com/app.tgz",
		Method:           http.MethodGet,
		SinkKind:         HTTPTransferSinkStdout,
		FollowsRedirects: true,
		FailOnHTTPStatus: true,
	}

	got, ok := op.LowerToTool(httpTransferToolCurl)
	if !ok {
		t.Fatal("expected lowering to curl when redirect and fail semantics are present")
	}
	if got != "curl -fL https://example.com/app.tgz" {
		t.Fatalf("curl lowering = %q, want %q", got, "curl -fL https://example.com/app.tgz")
	}

	op.FailOnHTTPStatus = false
	if _, ok := op.LowerToTool(httpTransferToolCurl); ok {
		t.Fatal("expected lowering to curl to fail without fail-on-http-status semantics")
	}

	op.FailOnHTTPStatus = true
	op.FollowsRedirects = false
	if _, ok := op.LowerToTool(httpTransferToolCurl); ok {
		t.Fatal("expected lowering to curl to fail without redirect-follow semantics")
	}
}

func TestHTTPTransferOperation_LowerToToolWindowsAddsExeSuffix(t *testing.T) {
	t.Parallel()

	op := &HTTPTransferOperation{
		URL:              "https://example.com/app.tgz",
		Method:           http.MethodGet,
		SinkKind:         HTTPTransferSinkStdout,
		FollowsRedirects: true,
		FailOnHTTPStatus: true,
	}

	winCurl, ok := op.LowerToTool(httpTransferToolCurl, HTTPTransferLowerOptions{WindowsTarget: true})
	if !ok {
		t.Fatal("expected curl lowering to succeed")
	}
	if winCurl != "curl.exe -fL https://example.com/app.tgz" {
		t.Fatalf("windows curl = %q, want curl.exe prefix", winCurl)
	}

	winWget, ok := op.LowerToTool(httpTransferToolWget, HTTPTransferLowerOptions{WindowsTarget: true})
	if !ok {
		t.Fatal("expected wget lowering to succeed")
	}
	if winWget != "wget.exe -O- https://example.com/app.tgz" {
		t.Fatalf("windows wget = %q, want wget.exe prefix", winWget)
	}

	linuxCurl, _ := op.LowerToTool(httpTransferToolCurl)
	if linuxCurl != "curl -fL https://example.com/app.tgz" {
		t.Fatalf("linux curl = %q, want bare curl", linuxCurl)
	}
}
