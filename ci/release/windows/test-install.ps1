[CmdletBinding()]
param(
    [Parameter(Mandatory)] [ValidateSet('User', 'Machine')] [string] $Scope,
    [Parameter(Mandatory)] [string] $Version
)

$ErrorActionPreference = 'Stop'
$userPrograms = Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'Programs\D2'
$machinePrograms = Join-Path ([Environment]::GetFolderPath('ProgramFiles')) 'D2'
if ($Scope -eq 'User') {
    $principal = [Security.Principal.WindowsPrincipal]::new([Security.Principal.WindowsIdentity]::GetCurrent())
    if ($principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw 'Run the user install test from a standard account.'
    }
    $directory, $otherDirectory, $otherScope = $userPrograms, $machinePrograms, 'Machine'
    $suffix, $registry, $displayName = '-user', 'HKCU:', 'D2 (current user)'
} else {
    $directory, $otherDirectory, $otherScope = $machinePrograms, $userPrograms, 'User'
    $suffix, $registry, $displayName = '', 'HKLM:', 'D2'
}

$currentMsi = Join-Path $PSScriptRoot "d2$suffix.msi"
$previousMsi = Join-Path $PSScriptRoot "previous$suffix.msi"
$uninstallKey = "$registry\Software\Microsoft\Windows\CurrentVersion\Uninstall"

function Invoke-MsiExec([string] $Operation, [string] $MsiPath) {
    $name = [IO.Path]::GetFileNameWithoutExtension($MsiPath)
    $log = Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) "$name-$Operation.log"
    $arguments = "/$Operation `"$MsiPath`" /qn /norestart /l*v `"$log`""
    $result = Start-Process msiexec.exe -ArgumentList $arguments -PassThru -Wait
    if ($result.ExitCode -notin @(0, 3010)) {
        if (Test-Path -LiteralPath $log) {
            Select-String -LiteralPath $log -Pattern 'Return value 3' -Context 8,0 | Out-Host
            Get-Content -LiteralPath $log -Tail 15 | Out-Host
        }
        throw "$Scope MSI /$Operation exited with $($result.ExitCode)"
    }
}

function Test-InstalledPath {
    $entries = [Environment]::GetEnvironmentVariable('Path', $Scope) -split ';'
    return ($entries | ForEach-Object { $_.TrimEnd('\') }) -contains $directory
}

if ((Test-Path -LiteralPath $directory) -or (Test-Path -LiteralPath $otherDirectory) -or (Test-InstalledPath)) {
    throw 'Run this test on a clean Windows image.'
}
$otherPath = [Environment]::GetEnvironmentVariable('Path', $otherScope)
Invoke-MsiExec 'i' $previousMsi
$previous = @(Get-ItemProperty "$uninstallKey\*" | Where-Object DisplayName -eq $displayName)
if ($previous.Count -ne 1 -or (Get-Content -LiteralPath "$directory\THIRD_PARTY_NOTICES.txt" -Raw).Trim() -ne 'upgrade fixture') {
    throw "Could not identify the existing $Scope installation"
}
Invoke-MsiExec 'i' $currentMsi
try {
    if (Test-Path -LiteralPath $previous[0].PSPath) { throw "Upgrade left the previous $Scope product registered" }
    foreach ($name in 'd2.exe', 'THIRD_PARTY_NOTICES.txt') {
        if (-not (Test-Path -LiteralPath (Join-Path $directory $name) -PathType Leaf)) {
            throw "Missing $directory\$name after $Scope install"
        }
    }
    $actualVersion = (& "$directory\d2.exe" --version 2>&1 | Out-String).Trim()
    if ($LASTEXITCODE -ne 0 -or $actualVersion -cne $Version) { throw "$Scope upgrade installed '$actualVersion'" }
    if (-not (Test-InstalledPath)) { throw "Missing $directory in $Scope PATH" }
    if ((Test-Path -LiteralPath $otherDirectory) -or
        $otherPath -cne [Environment]::GetEnvironmentVariable('Path', $otherScope)) {
        throw "$Scope install modified the $otherScope installation directory or PATH"
    }
} finally {
    Invoke-MsiExec 'x' $currentMsi
}
if ((Test-Path -LiteralPath $directory) -or (Test-InstalledPath) -or
    $otherPath -cne [Environment]::GetEnvironmentVariable('Path', $otherScope)) {
    throw "$Scope uninstall left installation files or PATH changes"
}
