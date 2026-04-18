package facts

import "testing"

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
