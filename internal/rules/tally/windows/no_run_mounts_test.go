package windows

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/wharflab/tally/internal/testutil"
)

func TestNoRunMountsRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewNoRunMountsRule().Metadata())
}

func TestNoRunMountsRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewNoRunMountsRule(), []testutil.RuleTestCase{
		{
			Name: "cache mount on windows",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN --mount=type=cache,target=C:\Users\ContainerUser\.nuget\packages dotnet restore
`,
			WantViolations: 1,
			WantCodes:      []string{NoRunMountsRuleCode},
			WantMessages:   []string{"cache"},
		},
		{
			Name: "secret mount on windows",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN --mount=type=secret,id=my_secret cmd /C type C:\run\secrets\my_secret
`,
			WantViolations: 1,
			WantMessages:   []string{"secret"},
		},
		{
			Name: "ssh mount on windows",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN --mount=type=ssh git clone git@github.com:org/repo.git
`,
			WantViolations: 1,
			WantMessages:   []string{"ssh"},
		},
		{
			Name: "bind mount on windows",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN --mount=type=bind,source=.,target=C:\src cmd /C dir C:\src
`,
			WantViolations: 1,
			WantMessages:   []string{"bind"},
		},
		{
			Name: "tmpfs mount on windows",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN --mount=type=tmpfs,target=C:\tmp cmd /C echo hi
`,
			WantViolations: 1,
			WantMessages:   []string{"tmpfs"},
		},
		{
			Name: "multiple mounts on same RUN",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN --mount=type=cache,target=C:\cache --mount=type=secret,id=key dotnet restore
`,
			WantViolations: 1,
			WantMessages:   []string{"cache,secret"},
		},
		{
			Name: "multiple RUN with mounts in same stage",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN --mount=type=cache,target=C:\cache dotnet restore
RUN --mount=type=secret,id=nuget_token dotnet build
`,
			WantViolations: 2,
		},
		{
			Name: "linux stage with mounts no violation",
			Content: `FROM ubuntu:22.04
RUN --mount=type=cache,target=/var/cache/apt apt-get update && apt-get install -y curl
`,
			WantViolations: 0,
		},
		{
			Name: "linux stage with pwsh shell and mount no violation",
			Content: `FROM ubuntu:22.04
SHELL ["pwsh", "-Command"]
RUN --mount=type=cache,target=/tmp/cache echo ok
`,
			WantViolations: 0,
		},
		{
			Name: "mixed stages only windows flagged",
			Content: `FROM ubuntu:22.04 AS builder
RUN --mount=type=cache,target=/root/.cache pip install -r requirements.txt

FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN --mount=type=cache,target=C:\cache dotnet restore
`,
			WantViolations: 1,
			WantMessages:   []string{"cache"},
		},
		{
			Name: "windows RUN without mounts no violation",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN powershell -Command Invoke-WebRequest https://example.com/file.zip -OutFile C:\temp\file.zip
`,
			WantViolations: 0,
		},
		{
			Name: "nanoserver with mount",
			Content: `FROM mcr.microsoft.com/windows/nanoserver:ltsc2022
RUN --mount=type=cache,target=C:\cache cmd /C echo hi
`,
			WantViolations: 1,
		},
		{
			Name: "exec form RUN with mount on windows",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN --mount=type=secret,id=token ["cmd", "/C", "type", "C:\\run\\secrets\\token"]
`,
			WantViolations: 1,
			WantMessages:   []string{"secret"},
		},
		{
			Name: "empty dockerfile",
			Content: `FROM scratch
`,
			WantViolations: 0,
		},
		{
			Name: "windows platform flag with mount",
			Content: `FROM --platform=windows/amd64 mcr.microsoft.com/dotnet/sdk:8.0
RUN --mount=type=cache,target=C:\nuget dotnet restore
`,
			WantViolations: 1,
		},
	})
}
