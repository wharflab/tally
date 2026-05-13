FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN net user app password /add && net localgroup docker app /add
USER app
