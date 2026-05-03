$ErrorActionPreference = 'Stop'
if ($null -ne (Get-Variable -Name PSStyle -ErrorAction SilentlyContinue)) {
    $PSStyle.OutputRendering = 'PlainText'
}

$script:TallyPSScriptAnalyzerVersion = $env:TALLY_PSSCRIPTANALYZER_VERSION
$script:TallyPSSAJsonStart = '--- tally PSSA JSON start ---'
$script:TallyPSSAJsonEnd = '--- tally PSSA JSON end ---'

function Write-JsonFrame {
    param([Parameter(Mandatory=$true)] [object] $Value)

    $json = $Value | ConvertTo-Json -Compress -Depth 8
    [Console]::Out.WriteLine($script:TallyPSSAJsonStart)
    [Console]::Out.WriteLine($json)
    [Console]::Out.WriteLine($script:TallyPSSAJsonEnd)
    [Console]::Out.Flush()
}

function Write-ProgressLine {
    param([Parameter(Mandatory=$true)] [string] $Message)

    Write-JsonFrame @{
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
            Version = $script:TallyPSScriptAnalyzerVersion
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
            RequiredVersion = $script:TallyPSScriptAnalyzerVersion
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
    if ([string]::IsNullOrWhiteSpace($script:TallyPSScriptAnalyzerVersion)) {
        throw 'TALLY_PSSCRIPTANALYZER_VERSION is not set.'
    }
    $script:TallyPSScriptAnalyzerVersion = $script:TallyPSScriptAnalyzerVersion.Trim()
    $requiredVersion = [Version] $script:TallyPSScriptAnalyzerVersion
    $available = @(Get-Module -ListAvailable -Name PSScriptAnalyzer | Where-Object { $_.Version -eq $requiredVersion })
    if ($available.Count -eq 0) {
        Write-ProgressLine "Installing PSScriptAnalyzer $script:TallyPSScriptAnalyzerVersion for PowerShell analyzer first use. This downloads the module into the selected pwsh CurrentUser scope."
        try {
            Install-PSScriptAnalyzerModule
        } catch {
            throw "PSScriptAnalyzer $script:TallyPSScriptAnalyzerVersion is not installed and automatic installation failed: $($_.Exception.Message)"
        }
    }

    try {
        Import-Module PSScriptAnalyzer -RequiredVersion $script:TallyPSScriptAnalyzerVersion -ErrorAction Stop
    } catch {
        throw "PSScriptAnalyzer $script:TallyPSScriptAnalyzerVersion could not be imported: $($_.Exception.Message)"
    }

    $module = Get-Module PSScriptAnalyzer
    if ($null -eq $module) {
        throw 'PSScriptAnalyzer import completed but no module instance was loaded.'
    }
    return $module
}

function Get-TallyAnalyzerSetting {
    param([object] $Request)

    if ($null -eq $Request.settings) {
        return $null
    }

    $settings = @{}
    if ($null -ne $Request.settings.includeRules -and @($Request.settings.includeRules).Count -gt 0) {
        $settings['IncludeRules'] = @($Request.settings.includeRules)
    }
    if ($null -ne $Request.settings.excludeRules -and @($Request.settings.excludeRules).Count -gt 0) {
        $settings['ExcludeRules'] = @($Request.settings.excludeRules)
    }
    if ($null -ne $Request.settings.severity -and @($Request.settings.severity).Count -gt 0) {
        $settings['Severity'] = @($Request.settings.severity)
    }
    if ($null -ne $Request.settings.rules) {
        $rules = ConvertTo-TallyHashtable -Value $Request.settings.rules
        if ($null -ne $rules -and $rules.Count -gt 0) {
            $settings['Rules'] = $rules
        }
    }
    if ($settings.Count -eq 0) {
        return $null
    }
    return $settings
}

function ConvertTo-TallyHashtable {
    param([object] $Value)

    if ($null -eq $Value) {
        return $null
    }

    if ($Value -is [System.Collections.IDictionary]) {
        $result = @{}
        foreach ($key in $Value.Keys) {
            $result[[string] $key] = ConvertTo-TallyHashtable -Value $Value[$key]
        }
        return $result
    }

    if ($Value -is [System.Management.Automation.PSCustomObject]) {
        $result = @{}
        foreach ($property in $Value.PSObject.Properties) {
            $result[$property.Name] = ConvertTo-TallyHashtable -Value $property.Value
        }
        return $result
    }

    if ($Value -is [System.Collections.IEnumerable] -and $Value -isnot [string]) {
        return @(
            foreach ($item in $Value) {
                ConvertTo-TallyHashtable -Value $item
            }
        )
    }

    return $Value
}

function ConvertTo-TallyDiagnostic {
    param([Parameter(Mandatory=$true)] [object] $Diagnostic)

    $extent = $Diagnostic.Extent
    $lineValue = $null
    $columnValue = $null
    $endLine = $null
    $endColumn = $null
    if ($null -ne $Diagnostic.Line -and $Diagnostic.Line -gt 0) {
        $lineValue = [int] $Diagnostic.Line
    }
    if ($null -ne $Diagnostic.Column -and $Diagnostic.Column -gt 0) {
        $columnValue = [int] $Diagnostic.Column
    }
    if ($null -ne $extent) {
        if ($extent.EndLineNumber -gt 0) {
            $endLine = [int] $extent.EndLineNumber
        }
        if ($extent.EndColumnNumber -gt 0) {
            $endColumn = [int] $extent.EndColumnNumber
        }
    }

    $suggestedCorrections = @()
    if ($null -ne $Diagnostic.SuggestedCorrections) {
        $suggestedCorrections = @(
            foreach ($correction in @($Diagnostic.SuggestedCorrections)) {
                if ($null -ne $correction) {
                    ConvertTo-TallyCorrection -Correction $correction
                }
            }
        )
    }

    return @{
        ruleName = [string] $Diagnostic.RuleName
        severity = [int] $Diagnostic.Severity
        line = $lineValue
        column = $columnValue
        endLine = $endLine
        endColumn = $endColumn
        message = [string] $Diagnostic.Message
        scriptPath = [string] $Diagnostic.ScriptPath
        suggestedCorrections = $suggestedCorrections
    }
}

function ConvertTo-TallyCorrection {
    param([Parameter(Mandatory=$true)] [object] $Correction)

    $text = ''
    if ($null -ne $Correction.Text) {
        $text = [string] $Correction.Text
    } elseif ($null -ne $Correction.Lines) {
        $text = [string]::Join([Environment]::NewLine, @($Correction.Lines))
    }

    return @{
        description = [string] $Correction.Description
        line = [int] $Correction.StartLineNumber
        column = [int] $Correction.StartColumnNumber
        endLine = [int] $Correction.EndLineNumber
        endColumn = [int] $Correction.EndColumnNumber
        text = $text
    }
}

function Invoke-TallyAnalyzeRequest {
    param([Parameter(Mandatory=$true)] [object] $Request)

    $params = @{}
    if ($null -ne $Request.path -and $Request.path -ne '') {
        $params['Path'] = [string] $Request.path
    } elseif ($null -ne $Request.scriptDefinition) {
        $params['ScriptDefinition'] = [string] $Request.scriptDefinition
    } else {
        throw 'an analyze request must include path or scriptDefinition'
    }

    $settings = Get-TallyAnalyzerSetting -Request $Request
    if ($null -ne $settings) {
        $params['Settings'] = $settings
    }

    $rawDiagnostics = @(Invoke-ScriptAnalyzer @params)
    $diagnostics = @(
        foreach ($diagnostic in $rawDiagnostics) {
            ConvertTo-TallyDiagnostic -Diagnostic $diagnostic
        }
    )

    return @{
        id = $Request.id
        ok = $true
        diagnostics = $diagnostics
    }
}

function Get-TallyFormatterSetting {
    $module = Get-Module PSScriptAnalyzer
    if ($null -eq $module) {
        throw 'PSScriptAnalyzer formatter settings requested before module import.'
    }

    $settingsPath = Join-Path $module.ModuleBase 'Settings/CodeFormatting.psd1'
    $settings = Import-PowerShellDataFile -LiteralPath $settingsPath

    # PSUseCorrectCasing depends on the cmdlets available in the host process.
    # Dockerfiles often target Windows images from Linux/macOS hosts, so keep
    # command casing stable instead of reflecting the sidecar host OS.
    if ($settings.ContainsKey('IncludeRules')) {
        $settings['IncludeRules'] = @(
            foreach ($rule in @($settings['IncludeRules'])) {
                if ($rule -ne 'PSUseCorrectCasing') {
                    $rule
                }
            }
        )
    }
    if ($settings.ContainsKey('Rules') -and $null -ne $settings['Rules'] -and $settings['Rules'].ContainsKey('PSUseCorrectCasing')) {
        $settings['Rules'].Remove('PSUseCorrectCasing')
    }

    return $settings
}

function Invoke-TallyFormatRequest {
    param([Parameter(Mandatory=$true)] [object] $Request)

    if ($null -eq $Request.scriptDefinition) {
        throw 'a format request must include scriptDefinition'
    }

    $formatted = Invoke-Formatter -ScriptDefinition ([string] $Request.scriptDefinition) -Settings (Get-TallyFormatterSetting)
    return @{
        id = $Request.id
        ok = $true
        formatted = [string] $formatted
    }
}

try {
    if ($PSVersionTable.PSVersion.Major -lt 7) {
        throw "PowerShell 7 or newer is required to run tally's PowerShell analyzer sidecar."
    }

    $module = Import-PSScriptAnalyzerModule

    Write-JsonFrame @{
        ready = $true
        version = $module.Version.ToString()
        ps = $PSVersionTable.PSVersion.ToString()
    }
} catch {
    Write-JsonFrame @{
        ready = $false
        error = $_.Exception.Message
    }
    exit 1
}

while ($null -ne ($line = [Console]::In.ReadLine())) {
    if ([string]::IsNullOrWhiteSpace($line)) {
        continue
    }

    $req = $null
    try {
        $req = $line | ConvertFrom-Json -ErrorAction Stop

        if ($req.op -eq 'shutdown') {
            Write-JsonFrame @{ id = $req.id; ok = $true }
            break
        }

        if ($req.op -eq 'analyze') {
            Write-JsonFrame (Invoke-TallyAnalyzeRequest -Request $req)
        } elseif ($req.op -eq 'format') {
            Write-JsonFrame (Invoke-TallyFormatRequest -Request $req)
        } else {
            throw "unsupported operation: $($req.op)"
        }
    } catch {
        $id = $null
        if ($null -ne $req) {
            $id = $req.id
        }
        Write-JsonFrame @{
            id = $id
            ok = $false
            error = $_.Exception.Message
        }
    }
}
