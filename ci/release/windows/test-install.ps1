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
    $suffix, $upgradeCode = '-user', '{B4061DE9-46B6-443A-966C-D4F6A9F1F7BF}'
} else {
    $directory, $otherDirectory, $otherScope = $machinePrograms, $userPrograms, 'User'
    $suffix, $upgradeCode = '', '{AC84FEE7-EB67-4F5D-A08D-ADEF69538690}'
}

$currentMsi = Join-Path $PSScriptRoot "d2$suffix.msi"
$previousMsi = Join-Path $PSScriptRoot "previous$suffix.msi"
$installer = New-Object -ComObject WindowsInstaller.Installer

function Get-D2Products {
    $products = $installer.GetType().InvokeMember('RelatedProducts', 'GetProperty', $null, $installer, @($upgradeCode))
    foreach ($code in $products) { $code }
}

function Invoke-MsiExec([string] $Operation, [string] $MsiPath) {
    $name = [IO.Path]::GetFileNameWithoutExtension($MsiPath)
    Write-Host "Running msiexec /$Operation $name.msi as $([Security.Principal.WindowsIdentity]::GetCurrent().Name)"
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
$previous = @(Get-D2Products)
if ($previous.Count -ne 1 -or (Get-Content -LiteralPath "$directory\THIRD_PARTY_NOTICES.txt" -Raw).Trim() -ne 'upgrade fixture') {
    throw "Could not identify the existing $Scope installation"
}
Invoke-MsiExec 'i' $currentMsi
try {
    $current = @(Get-D2Products)
    if ($current.Count -ne 1 -or $current[0] -eq $previous[0]) { throw "Upgrade did not replace the previous $Scope product" }
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
if (@(Get-D2Products).Count -or (Test-Path -LiteralPath $directory) -or (Test-InstalledPath) -or
    $otherPath -cne [Environment]::GetEnvironmentVariable('Path', $otherScope)) {
    throw "$Scope uninstall left installation files or PATH changes"
}
