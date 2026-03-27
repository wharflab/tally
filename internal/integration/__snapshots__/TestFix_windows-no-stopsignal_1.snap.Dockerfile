FROM mcr.microsoft.com/windows/servercore:ltsc2022
# [commented out by tally - STOPSIGNAL has no effect on Windows containers]: STOPSIGNAL SIGTERM
CMD ["cmd", "/C", "echo", "hi"]
