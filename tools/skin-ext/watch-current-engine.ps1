param(
    [Parameter(Mandatory = $true)]
    [int]$EngineProcessId
)

$ErrorActionPreference = 'SilentlyContinue'
$skinExecutable = Join-Path $PSScriptRoot '..\..\runtime\codex-gearth\extensions\skin.exe'
$figure = 'hd-209-1005.ch-210-1281.lg-285-1281.sh-300-1281.hr-170-1110.ha-1007-1289'
$knownPorts = [System.Collections.Generic.HashSet[int]]::new()
$firstScan = $true

while (Get-Process -Id $EngineProcessId -ErrorAction SilentlyContinue) {
    $activePorts = [System.Collections.Generic.HashSet[int]]::new()
    $processes = Get-CimInstance Win32_Process | Where-Object {
        $_.Name -eq 'java.exe' -and $_.CommandLine -like '*gearth.app.CodexGEarthMain*'
    }

    foreach ($process in $processes) {
        if ($process.CommandLine -notmatch '--extension-port\s+(\d+)') {
            continue
        }
        $port = [int]$Matches[1]
        [void]$activePorts.Add($port)
        if ($firstScan) {
            [void]$knownPorts.Add($port)
            continue
        }
        if ($knownPorts.Add($port)) {
            Start-Process -FilePath $skinExecutable `
                -ArgumentList @($port, $figure) `
                -WindowStyle Hidden
        }
    }

    foreach ($port in @($knownPorts)) {
        if (-not $activePorts.Contains($port)) {
            [void]$knownPorts.Remove($port)
        }
    }
    $firstScan = $false
    Start-Sleep -Seconds 2
}
