//go:build containers_image_openpgp && containers_image_storage_stub && containers_image_docker_daemon_stub

package registry

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/docker/distribution/registry/api/errcode"
	v2 "github.com/docker/distribution/registry/api/v2"
	"go.podman.io/image/v5/docker"
)

// fakeNetError implements net.Error for testing.
type fakeNetError struct{ msg string }

func (e *fakeNetError) Error() string   { return e.msg }
func (e *fakeNetError) Timeout() bool   { return true }
func (e *fakeNetError) Temporary() bool { return true }

func TestClassifyContainersError_Nil(t *testing.T) {
	t.Parallel()
	if err := classifyContainersError("ref", nil); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestClassifyContainersError_ContextDeadlineExceeded(t *testing.T) {
	t.Parallel()
	err := classifyContainersError("myimage:latest", context.DeadlineExceeded)
	var netErr *NetworkError
	if !errors.As(err, &netErr) {
		t.Fatalf("expected *NetworkError, got %T: %v", err, err)
	}
}

func TestClassifyContainersError_ContextCanceled(t *testing.T) {
	t.Parallel()
	err := classifyContainersError("myimage:latest", context.Canceled)
	var netErr *NetworkError
	if !errors.As(err, &netErr) {
		t.Fatalf("expected *NetworkError, got %T: %v", err, err)
	}
}

func TestClassifyContainersError_WrappedContextError(t *testing.T) {
	t.Parallel()
	wrapped := fmt.Errorf("fetching manifest: %w", context.DeadlineExceeded)
	err := classifyContainersError("myimage:latest", wrapped)
	var netErr *NetworkError
	if !errors.As(err, &netErr) {
		t.Fatalf("expected *NetworkError, got %T: %v", err, err)
	}
}

func TestClassifyContainersError_ErrUnauthorizedForCredentials(t *testing.T) {
	t.Parallel()
	err := classifyContainersError("private:latest",
		docker.ErrUnauthorizedForCredentials{Err: errors.New("bad creds")})
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *AuthError, got %T: %v", err, err)
	}
}

func TestClassifyContainersError_WrappedErrUnauthorizedForCredentials(t *testing.T) {
	t.Parallel()
	inner := docker.ErrUnauthorizedForCredentials{Err: errors.New("bad creds")}
	wrapped := fmt.Errorf("auth failed: %w", inner)
	err := classifyContainersError("private:latest", wrapped)
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *AuthError, got %T: %v", err, err)
	}
}

func TestClassifyContainersError_ErrTooManyRequests(t *testing.T) {
	t.Parallel()
	err := classifyContainersError("image:latest", docker.ErrTooManyRequests)
	var netErr *NetworkError
	if !errors.As(err, &netErr) {
		t.Fatalf("expected *NetworkError, got %T: %v", err, err)
	}
}

func TestClassifyContainersError_ErrorCodeUnauthorized(t *testing.T) {
	t.Parallel()
	err := classifyContainersError("image:latest",
		errcode.ErrorCodeUnauthorized.WithMessage("authentication required"))
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *AuthError, got %T: %v", err, err)
	}
}

func TestClassifyContainersError_ErrorCodeDenied(t *testing.T) {
	t.Parallel()
	err := classifyContainersError("image:latest",
		errcode.ErrorCodeDenied.WithMessage("access denied"))
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *AuthError, got %T: %v", err, err)
	}
}

func TestClassifyContainersError_ErrorCodeManifestUnknown(t *testing.T) {
	t.Parallel()
	err := classifyContainersError("image:latest",
		v2.ErrorCodeManifestUnknown.WithMessage("manifest unknown to registry"))
	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *NotFoundError, got %T: %v", err, err)
	}
	if notFound.Ref != "image:latest" {
		t.Errorf("expected ref %q, got %q", "image:latest", notFound.Ref)
	}
}

func TestClassifyContainersError_ErrorCodeNameUnknown(t *testing.T) {
	t.Parallel()
	err := classifyContainersError("nosuchimage:latest",
		v2.ErrorCodeNameUnknown.WithMessage("repository name not known"))
	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *NotFoundError, got %T: %v", err, err)
	}
}

func TestClassifyContainersError_ErrorCodeBlobUnknown(t *testing.T) {
	t.Parallel()
	err := classifyContainersError("image:latest",
		v2.ErrorCodeBlobUnknown.WithMessage("blob unknown"))
	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *NotFoundError, got %T: %v", err, err)
	}
}

func TestClassifyContainersError_ErrorCodeTooManyRequests(t *testing.T) {
	t.Parallel()
	err := classifyContainersError("image:latest",
		errcode.ErrorCodeTooManyRequests.WithMessage("rate limited"))
	var netErr *NetworkError
	if !errors.As(err, &netErr) {
		t.Fatalf("expected *NetworkError, got %T: %v", err, err)
	}
}

func TestClassifyContainersError_ErrorCodeUnavailable(t *testing.T) {
	t.Parallel()
	err := classifyContainersError("image:latest",
		errcode.ErrorCodeUnavailable.WithMessage("service unavailable"))
	var netErr *NetworkError
	if !errors.As(err, &netErr) {
		t.Fatalf("expected *NetworkError, got %T: %v", err, err)
	}
}

func TestClassifyContainersError_UnexpectedHTTPStatus401(t *testing.T) {
	t.Parallel()
	err := classifyContainersError("image:latest",
		docker.UnexpectedHTTPStatusError{StatusCode: 401})
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *AuthError, got %T: %v", err, err)
	}
}

func TestClassifyContainersError_UnexpectedHTTPStatus403(t *testing.T) {
	t.Parallel()
	err := classifyContainersError("image:latest",
		docker.UnexpectedHTTPStatusError{StatusCode: 403})
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *AuthError, got %T: %v", err, err)
	}
}

func TestClassifyContainersError_UnexpectedHTTPStatus404(t *testing.T) {
	t.Parallel()
	err := classifyContainersError("image:latest",
		docker.UnexpectedHTTPStatusError{StatusCode: 404})
	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *NotFoundError, got %T: %v", err, err)
	}
}

func TestClassifyContainersError_UnexpectedHTTPStatus500(t *testing.T) {
	t.Parallel()
	err := classifyContainersError("image:latest",
		docker.UnexpectedHTTPStatusError{StatusCode: 500})
	var netErr *NetworkError
	if !errors.As(err, &netErr) {
		t.Fatalf("expected *NetworkError, got %T: %v", err, err)
	}
}

func TestClassifyContainersError_NetError(t *testing.T) {
	t.Parallel()
	err := classifyContainersError("image:latest", &fakeNetError{msg: "connection reset"})
	var netErr *NetworkError
	if !errors.As(err, &netErr) {
		t.Fatalf("expected *NetworkError, got %T: %v", err, err)
	}
}

func TestClassifyContainersError_WrappedNetError(t *testing.T) {
	t.Parallel()
	inner := &fakeNetError{msg: "connection refused"}
	wrapped := fmt.Errorf("dial tcp: %w", inner)
	err := classifyContainersError("image:latest", wrapped)
	var netErr *NetworkError
	if !errors.As(err, &netErr) {
		t.Fatalf("expected *NetworkError, got %T: %v", err, err)
	}
}

func TestClassifyContainersError_StringFallback_Unauthorized(t *testing.T) {
	t.Parallel()
	// Plain error with no typed info, only string content.
	err := classifyContainersError("image:latest", errors.New("unauthorized access"))
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *AuthError via string fallback, got %T: %v", err, err)
	}
}

func TestClassifyContainersError_StringFallback_NotFound(t *testing.T) {
	t.Parallel()
	err := classifyContainersError("image:latest", errors.New("manifest unknown"))
	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *NotFoundError via string fallback, got %T: %v", err, err)
	}
}

func TestClassifyContainersError_UnknownError_DefaultsToNetwork(t *testing.T) {
	t.Parallel()
	err := classifyContainersError("image:latest", errors.New("something unexpected"))
	var netErr *NetworkError
	if !errors.As(err, &netErr) {
		t.Fatalf("expected *NetworkError as default, got %T: %v", err, err)
	}
}

// Verify that net.Error interface is properly satisfied by fakeNetError.
var _ net.Error = (*fakeNetError)(nil) //nolint:errcheck // compile-time interface assertion, not an error return
