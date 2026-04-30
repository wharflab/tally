$ErrorActionPreference = 'Stop'

function Write-JsonLine {
    param([Parameter(Mandatory=$true)] [object] $Value)

    $json = $Value | ConvertTo-Json -Compress -Depth 8
    [Console]::Out.WriteLine($json)
    [Console]::Out.Flush()
}

function Write-ProgressLine {
    param([Parameter(Mandatory=$true)] [string] $Message)

    Write-JsonLine @{
        progress = $true
        message = $Message
    }
}

function Install-PSScriptAnalyzerModule {
    $installPSResource = Get-Command -Name Install-PSResource -ErrorAction SilentlyContinue
    if ($null -ne $installPSResource) {
        $params = @{
            Name = 'PSScriptAnalyzer'
            Scope = 'CurrentUser'
            ErrorAction = 'Stop'
        }
        if ($installPSResource.Parameters.ContainsKey('TrustRepository')) {
            $params['TrustRepository'] = $true
        }
        if ($installPSResource.Parameters.ContainsKey('AcceptLicense')) {
            $params['AcceptLicense'] = $true
        }
        if ($installPSResource.Parameters.ContainsKey('Quiet')) {
            $params['Quiet'] = $true
        }
        Install-PSResource @params | Out-Null
        return
    }

    $installModule = Get-Command -Name Install-Module -ErrorAction SilentlyContinue
    if ($null -ne $installModule) {
        $params = @{
            Name = 'PSScriptAnalyzer'
            Scope = 'CurrentUser'
            Force = $true
            ErrorAction = 'Stop'
        }
        if ($installModule.Parameters.ContainsKey('AcceptLicense')) {
            $params['AcceptLicense'] = $true
        }
        if ($installModule.Parameters.ContainsKey('AllowClobber')) {
            $params['AllowClobber'] = $true
        }
        if ($installModule.Parameters.ContainsKey('SkipPublisherCheck')) {
            $params['SkipPublisherCheck'] = $true
        }
        Install-Module @params | Out-Null
        return
    }

    throw 'PSScriptAnalyzer is not installed, and neither Install-PSResource nor Install-Module is available in this pwsh environment.'
}

function Import-PSScriptAnalyzerModule {
    $available = @(Get-Module -ListAvailable -Name PSScriptAnalyzer)
    if ($available.Count -eq 0) {
        Write-ProgressLine 'Installing PSScriptAnalyzer for PowerShell analyzer first use. This downloads the module into the selected pwsh CurrentUser scope.'
        try {
            Install-PSScriptAnalyzerModule
        } catch {
            throw "PSScriptAnalyzer is not installed and automatic installation failed: $($_.Exception.Message)"
        }
    }

    try {
        Import-Module PSScriptAnalyzer -ErrorAction Stop
    } catch {
        throw "PSScriptAnalyzer could not be imported: $($_.Exception.Message)"
    }

    $module = Get-Module PSScriptAnalyzer
    if ($null -eq $module) {
        throw 'PSScriptAnalyzer import completed but no module instance was loaded.'
    }
    return $module
}

try {
    if ($PSVersionTable.PSVersion.Major -lt 7) {
        throw "PowerShell 7 or newer is required to run tally's PowerShell analyzer sidecar."
    }

    $module = Import-PSScriptAnalyzerModule

    Write-JsonLine @{
        ready = $true
        version = $module.Version.ToString()
        ps = $PSVersionTable.PSVersion.ToString()
    }
} catch {
    Write-JsonLine @{
        ready = $false
        error = $_.Exception.Message
    }
    exit 1
}

while (($line = [Console]::In.ReadLine()) -ne $null) {
    if ([string]::IsNullOrWhiteSpace($line)) {
        continue
    }

    $req = $null
    try {
        $req = $line | ConvertFrom-Json -ErrorAction Stop

        if ($req.op -eq 'shutdown') {
            Write-JsonLine @{ id = $req.id; ok = $true }
            break
        }

        if ($req.op -ne 'analyze') {
            throw "unsupported operation: $($req.op)"
        }

        $params = @{}
        if ($null -ne $req.path -and $req.path -ne '') {
            $params['Path'] = [string] $req.path
        } elseif ($null -ne $req.scriptDefinition) {
            $params['ScriptDefinition'] = [string] $req.scriptDefinition
        } else {
            throw 'an analyze request must include path or scriptDefinition'
        }

        if ($null -ne $req.settings) {
            $settings = @{}
            if ($null -ne $req.settings.includeRules -and @($req.settings.includeRules).Count -gt 0) {
                $settings['IncludeRules'] = @($req.settings.includeRules)
            }
            if ($null -ne $req.settings.excludeRules -and @($req.settings.excludeRules).Count -gt 0) {
                $settings['ExcludeRules'] = @($req.settings.excludeRules)
            }
            if ($null -ne $req.settings.severity -and @($req.settings.severity).Count -gt 0) {
                $settings['Severity'] = @($req.settings.severity)
            }
            if ($settings.Count -gt 0) {
                $params['Settings'] = $settings
            }
        }

        $rawDiagnostics = @(Invoke-ScriptAnalyzer @params)
        $diagnostics = @(
            foreach ($d in $rawDiagnostics) {
                $extent = $d.Extent
                $lineValue = $null
                $columnValue = $null
                $endLine = $null
                $endColumn = $null
                if ($null -ne $d.Line -and $d.Line -gt 0) {
                    $lineValue = [int] $d.Line
                }
                if ($null -ne $d.Column -and $d.Column -gt 0) {
                    $columnValue = [int] $d.Column
                }
                if ($null -ne $extent) {
                    if ($extent.EndLineNumber -gt 0) {
                        $endLine = [int] $extent.EndLineNumber
                    }
                    if ($extent.EndColumnNumber -gt 0) {
                        $endColumn = [int] $extent.EndColumnNumber
                    }
                }

                @{
                    ruleName = [string] $d.RuleName
                    severity = [int] $d.Severity
                    line = $lineValue
                    column = $columnValue
                    endLine = $endLine
                    endColumn = $endColumn
                    message = [string] $d.Message
                    scriptPath = [string] $d.ScriptPath
                }
            }
        )

        Write-JsonLine @{
            id = $req.id
            ok = $true
            diagnostics = $diagnostics
        }
    } catch {
        $id = $null
        if ($null -ne $req) {
            $id = $req.id
        }
        Write-JsonLine @{
            id = $id
            ok = $false
            error = $_.Exception.Message
        }
    }
}
