# Test that shell-specific rules are disabled for non-POSIX shells
# hadolint shell=powershell
FROM mcr.microsoft.com/windows/servercore:ltsc2022

# This would trigger DL3004 (sudo) if shell analysis was active
# but should be ignored because PowerShell is non-POSIX
RUN Get-Process notepad | Stop-Process

# This would trigger DL4001 (wget and curl) if shell analysis was active
# but should be ignored because PowerShell is non-POSIX
RUN wget https://example.com/file1
RUN curl https://example.com/file2
