# Installing BuildKit For Windows Containers

This guide sets up a standalone BuildKit daemon for Windows containers and connects `docker buildx` to it with the `remote` driver.

It is based on a working setup validated on **April 1, 2026** with real Windows container builds.

## Known-good versions

- `containerd v1.7.30`
- `buildkit v0.29.0`
- `buildx v0.33.0`

Do **not** use `containerd v2.2.2` for this setup. In our Windows validation, `buildkitd` failed to start against it with:

```text
unknown service containerd.services.leases.v1.Leases
```

## Prerequisites

- Windows with Docker Desktop installed
- Docker Desktop switched to **Windows containers**
- An **elevated PowerShell** session
- Windows container prerequisites enabled (`Hyper-V`, Windows Containers, and the default `nat` network)

## Install standalone BuildKit

Run this in an elevated PowerShell window:

```powershell
$ErrorActionPreference = 'Stop'

$containerdVersion = '1.7.30'
$buildkitVersion = 'v0.29.0'
$buildxVersion = 'v0.33.0'
$arch = 'amd64'

$temp = Join-Path $env:TEMP 'buildkit-win-upgrade'
New-Item -ItemType Directory -Force -Path $temp | Out-Null
Set-Location $temp

Write-Host 'Checking nat network...'
$nat = Get-HnsNetwork | Where-Object { $_.Name -eq 'nat' }
if ($null -eq $nat) {
  throw "NAT network not found. Make sure Windows Containers / Hyper-V are enabled and Docker is in Windows containers mode."
}
$gateway = $nat.Subnets[0].GatewayAddress
$subnet = $nat.Subnets[0].AddressPrefix

Write-Host 'Installing CNI config...'
$cniConfPath = "$env:ProgramFiles\containerd\cni\conf"
$cniBinDir = "$env:ProgramFiles\containerd\cni\bin"
$cniVersion = '0.3.0'
New-Item -ItemType Directory -Force -Path $cniConfPath, $cniBinDir | Out-Null
curl.exe -fSL "https://github.com/microsoft/windows-container-networking/releases/download/v$cniVersion/windows-container-networking-cni-amd64-v$cniVersion.zip" -o "windows-container-networking-cni-amd64-v$cniVersion.zip"
tar.exe xvf "windows-container-networking-cni-amd64-v$cniVersion.zip" -C $cniBinDir

$natConfig = @"
{
  "cniVersion": "$cniVersion",
  "name": "nat",
  "type": "nat",
  "master": "Ethernet",
  "ipam": {
    "subnet": "$subnet",
    "routes": [
      {
        "gateway": "$gateway"
      }
    ]
  },
  "capabilities": {
    "portMappings": true,
    "dns": true
  }
}
"@
Set-Content -Path "$cniConfPath\0-containerd-nat.conf" -Value $natConfig

Write-Host 'Installing containerd...'
curl.exe -fSL "https://github.com/containerd/containerd/releases/download/v$containerdVersion/containerd-$containerdVersion-windows-$arch.tar.gz" -o "containerd-$containerdVersion-windows-$arch.tar.gz"
tar.exe xvf "containerd-$containerdVersion-windows-$arch.tar.gz"

if (Get-Service containerd -ErrorAction SilentlyContinue) {
  Stop-Service containerd -ErrorAction SilentlyContinue
  .\bin\containerd.exe --unregister-service
}
.\bin\containerd.exe --register-service
Start-Service containerd

Write-Host 'Installing BuildKit...'
curl.exe -fSL "https://github.com/moby/buildkit/releases/download/$buildkitVersion/buildkit-$buildkitVersion.windows-$arch.tar.gz" -o "buildkit-$buildkitVersion.windows-$arch.tar.gz"
tar.exe xvf "buildkit-$buildkitVersion.windows-$arch.tar.gz"

$buildkitPath = "$env:ProgramFiles\buildkit"
New-Item -ItemType Directory -Force -Path $buildkitPath | Out-Null
Copy-Item -Path ".\bin\*" -Destination $buildkitPath -Force

if (Get-Service buildkitd -ErrorAction SilentlyContinue) {
  Stop-Service buildkitd -ErrorAction SilentlyContinue
  & "$buildkitPath\buildkitd.exe" --unregister-service
}

Write-Host 'Registering buildkitd service...'
& "$buildkitPath\buildkitd.exe" --register-service `
  --addr "npipe:////./pipe/buildkitd" `
  --containerd-cni-config-path="$cniConfPath\0-containerd-nat.conf" `
  --containerd-cni-binary-dir="$cniBinDir"

Start-Service buildkitd

Write-Host 'Installing buildx plugin...'
$pluginsDir = "$env:USERPROFILE\.docker\cli-plugins"
New-Item -ItemType Directory -Force -Path $pluginsDir | Out-Null
curl.exe -fSL "https://github.com/docker/buildx/releases/download/$buildxVersion/buildx-$buildxVersion.windows-$arch.exe" -o "$pluginsDir\docker-buildx.exe"

Write-Host 'Waiting for buildkitd...'
for ($i = 0; $i -lt 30; $i++) {
  Start-Sleep -Seconds 2
  & "$buildkitPath\buildctl.exe" --addr "npipe:////./pipe/buildkitd" debug info *> $null
  if ($LASTEXITCODE -eq 0) { break }
}
& "$buildkitPath\buildctl.exe" --addr "npipe:////./pipe/buildkitd" debug info

Write-Host 'Creating buildx builder...'
docker buildx rm buildkit-windows 2>$null
docker buildx create --name buildkit-windows --driver remote "npipe:////./pipe/buildkitd" --use
docker buildx inspect --bootstrap buildkit-windows
docker buildx version
```

## Allow non-admin clients to use the BuildKit pipe

If `buildctl` works in the elevated shell but fails in a normal shell with `Access is denied`, re-register `buildkitd` like this:

```powershell
Stop-Service buildkitd

& "$env:ProgramFiles\buildkit\buildkitd.exe" --unregister-service

& "$env:ProgramFiles\buildkit\buildkitd.exe" --register-service `
  --addr "npipe:////./pipe/buildkitd" `
  --group "Users" `
  --containerd-cni-config-path="$env:ProgramFiles\containerd\cni\conf\0-containerd-nat.conf" `
  --containerd-cni-binary-dir="$env:ProgramFiles\containerd\cni\bin"

Start-Service buildkitd
```

## Verify the builder

Run these in a normal PowerShell session:

```powershell
docker buildx version
docker buildx inspect buildkit-windows --bootstrap
& "$env:ProgramFiles\buildkit\buildctl.exe" --addr "npipe:////./pipe/buildkitd" debug info
```

Expected checks:

- `docker buildx version` shows `v0.33.0`
- `docker buildx inspect buildkit-windows --bootstrap` shows `Driver: remote`
- the node reports `BuildKit version: v0.29.0`
- `buildctl debug info` succeeds through `\\.\pipe\buildkitd`

## Smoke test with a real Windows build

```powershell
$testDir = Join-Path $env:TEMP 'buildkit-win-test'
New-Item -ItemType Directory -Force -Path $testDir | Out-Null

@'
FROM mcr.microsoft.com/windows/nanoserver:ltsc2025
RUN cmd /S /C echo upgraded-buildkit> C:\buildkit.txt
'@ | Set-Content -Path (Join-Path $testDir 'Dockerfile')

docker buildx build --builder buildkit-windows --platform windows/amd64 --progress plain --load -t bk-win-test $testDir
docker run --rm bk-win-test cmd /S /C type C:\buildkit.txt
```

If the setup is healthy, the container prints:

```text
upgraded-buildkit
```

## Troubleshooting

`buildkitd` fails to start with `unknown service containerd.services.leases.v1.Leases`

- You are likely using an incompatible `containerd` version such as `2.2.2`.
- Reinstall `containerd v1.7.30`.

`buildx inspect buildkit-windows --bootstrap` times out

- Check that the `buildkitd` service is running.
- Check that `buildctl debug info` works from the same shell.
- If only the elevated shell works, re-register `buildkitd` with `--group "Users"`.

`buildctl` says `Access is denied`

- The named pipe permissions are too restrictive.
- Re-register `buildkitd` with `--group "Users"` as shown above.

Windows build starts but fails during execution

- Make sure Docker Desktop is actually in Windows containers mode.
- Confirm the builder reports `Platforms: windows/amd64`.
- Re-run the smoke test before debugging your own Dockerfile.
