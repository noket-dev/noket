<#
.SYNOPSIS
    Installs noket and noketd on Windows and registers the agent at logon.

.DESCRIPTION
    Downloads a signed release archive for this machine, verifies its SHA-256
    checksum, installs noket.exe and noketd.exe, adds them to the user PATH,
    and registers a Scheduled Task that runs `noket up --foreground` at logon.

.EXAMPLE
    irm https://raw.githubusercontent.com/noket-dev/noket/main/install.ps1 | iex

.EXAMPLE
    .\install.ps1 -Version v0.1.0 -InstallDir C:\Tools\noket -NoAutostart

.EXAMPLE
    .\install.ps1 -Uninstall -Purge
#>
[CmdletBinding()]
param(
    [string]$Version = $env:NOKET_VERSION,
    [string]$InstallDir = $env:NOKET_INSTALL_DIR,
    [switch]$NoAutostart,
    [switch]$NoLogin,
    [switch]$Uninstall,
    [switch]$Purge
)

$Repo = 'noket-dev/noket'
$TaskName = 'Noket'
$ConfigDir = if ($env:NOKET_CONFIG_DIR) { $env:NOKET_CONFIG_DIR } else { Join-Path $env:USERPROFILE '.noket' }

if (-not $InstallDir) {
    $InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\noket'
}
$InstallDir = [IO.Path]::GetFullPath($InstallDir)
$NoketExe = Join-Path $InstallDir 'noket.exe'
$NoketdExe = Join-Path $InstallDir 'noketd.exe'

function Write-Step([string]$Message) {
    Write-Host "==> $Message" -ForegroundColor Green
}

function Write-Note([string]$Message) {
    Write-Host "==> $Message" -ForegroundColor DarkGray
}

function Write-Warn([string]$Message) {
    Write-Host "==> warning: $Message" -ForegroundColor Yellow
}

function Stop-Installer([string]$Message) {
    # Throw instead of exit: under `irm | iex`, exit would close the user's
    # PowerShell session.
    throw "noket: $Message"
}

function Get-NoketArch {
    $arch = $env:PROCESSOR_ARCHITECTURE
    if ($env:PROCESSOR_ARCHITEW6432) {
        $arch = $env:PROCESSOR_ARCHITEW6432
    }
    switch -Regex ($arch) {
        '^(AMD64)$' { return 'amd64' }
        '^(x86)$' { return '386' }
        '^(ARM64)$' { return 'arm64' }
        default { Stop-Installer "unsupported architecture: $arch" }
    }
}

function Resolve-NoketVersion([string]$Requested) {
    if (-not $Requested) {
        Write-Step 'Looking up the latest release'
        try {
            $release = Invoke-RestMethod -UseBasicParsing -Headers @{ 'User-Agent' = 'noket-installer' } `
                -Uri "https://api.github.com/repos/$Repo/releases/latest"
            $Requested = $release.tag_name
        }
        catch {
            Stop-Installer 'could not determine the latest version; pass -Version vX.Y.Z'
        }
    }
    if (-not $Requested.StartsWith('v')) {
        $Requested = "v$Requested"
    }
    return $Requested
}

function Get-NoketFile([string]$Uri, [string]$OutFile) {
    Invoke-WebRequest -UseBasicParsing -Uri $Uri -OutFile $OutFile
}

function Assert-NoketChecksum([string]$File, [string]$List, [string]$Name) {
    $expected = $null
    foreach ($line in Get-Content -LiteralPath $List) {
        $parts = $line -split '\s+', 2
        if ($parts.Count -eq 2 -and $parts[1].Trim() -eq $Name) {
            $expected = $parts[0].Trim()
            break
        }
    }
    if (-not $expected) {
        Stop-Installer "checksums.txt has no entry for $Name"
    }
    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $File).Hash
    if ($expected.ToLowerInvariant() -ne $actual.ToLowerInvariant()) {
        Stop-Installer "checksum mismatch for $Name"
    }
    Write-Note 'Checksum verified'
}

function Stop-NoketProcesses {
    Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    Get-Process -Name 'noket', 'noketd' -ErrorAction SilentlyContinue | ForEach-Object {
        $process = $_
        try {
            $process.Kill()
        }
        catch {
            Write-Warn "could not stop $($process.ProcessName) (pid $($process.Id))"
        }
    }
}

function Add-NoketToUserPath([string]$Dir) {
    $current = [Environment]::GetEnvironmentVariable('Path', 'User')
    $parts = @()
    if ($current) {
        $parts = @($current -split ';' | Where-Object { $_ })
    }
    if ($parts -contains $Dir) {
        Write-Note "$Dir is already on PATH"
        return
    }
    $updated = (@($parts) + $Dir) -join ';'
    [Environment]::SetEnvironmentVariable('Path', $updated, 'User')
    if (-not (($env:Path -split ';') -contains $Dir)) {
        $env:Path = "$Dir;$env:Path"
    }
    Write-Note "Added $Dir to the user PATH"
}

function Remove-NoketFromUserPath([string]$Dir) {
    $current = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (-not $current) {
        return
    }
    $parts = @($current -split ';' | Where-Object { $_ -and $_ -ne $Dir })
    if ($parts.Count -eq 0) {
        [Environment]::SetEnvironmentVariable('Path', $null, 'User')
    }
    else {
        [Environment]::SetEnvironmentVariable('Path', ($parts -join ';'), 'User')
    }
}

function Invoke-NoketLogin {
    if ($NoLogin) {
        return
    }
    $creds = Join-Path $ConfigDir 'credentials.json'
    if (Test-Path -LiteralPath $creds) {
        Write-Note 'Already logged in'
        return
    }
    Write-Step 'No login found; starting the browser login'
    & $NoketExe login
    if ($LASTEXITCODE -ne 0) {
        Write-Warn "login did not finish; run 'noket login' later"
    }
}

function Enable-NoketAutostart {
    if ($NoAutostart) {
        Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false -ErrorAction SilentlyContinue
        Write-Note 'Autostart disabled (-NoAutostart)'
        return
    }

    $userId = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name
    $escapedExe = $NoketExe.Replace("'", "''")
    $logPath = Join-Path $ConfigDir 'noketd.log'
    $escapedLog = $logPath.Replace("'", "''")
    New-Item -ItemType Directory -Force -Path $ConfigDir | Out-Null

    # The wrapper keeps the console window hidden; *>> appends daemon output to
    # the log the CLI's `noket status` and docs refer to.
    $inner = "& '$escapedExe' up --foreground *>> '$escapedLog'"
    $arguments = "-NoProfile -NonInteractive -WindowStyle Hidden -Command `"$inner`""

    $action = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument $arguments

    # AtLogOn starts the agent after a boot; the repetition retries it when it
    # exited because the user had not logged in yet. IgnoreNew keeps the retry
    # from starting a second daemon while the first is connected.
    $triggers = @(
        (New-ScheduledTaskTrigger -AtLogOn -User $userId),
        (New-ScheduledTaskTrigger -Once -At (Get-Date).AddMinutes(1) `
                -RepetitionInterval (New-TimeSpan -Minutes 5) `
                -RepetitionDuration (New-TimeSpan -Days 3650))
    )
    $settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries `
        -StartWhenAvailable -MultipleInstances IgnoreNew `
        -ExecutionTimeLimit ([TimeSpan]::Zero)
    $principal = New-ScheduledTaskPrincipal -UserId $userId -LogonType Interactive -RunLevel Limited

    try {
        Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $triggers `
            -Settings $settings -Principal $principal -Force | Out-Null
        Start-ScheduledTask -TaskName $TaskName
        Write-Step "Agent enabled (Scheduled Task: $TaskName); it starts at logon"
    }
    catch {
        Write-Warn "could not register the Scheduled Task: $($_.Exception.Message)"
        Write-Warn "start the agent manually with: `"$NoketExe`" up"
    }
}

function Install-Noket {
    $arch = Get-NoketArch
    $tag = Resolve-NoketVersion $Version
    $archiveVersion = $tag.TrimStart('v')
    $asset = "noket_windows_${arch}_${archiveVersion}.zip"
    $base = "https://github.com/$Repo/releases/download/$tag"

    Stop-NoketProcesses

    $tmp = Join-Path ([IO.Path]::GetTempPath()) ('noket-install-' + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        Write-Step "Downloading noket $tag for windows/$arch"
        $zip = Join-Path $tmp $asset
        $checksums = Join-Path $tmp 'checksums.txt'
        Get-NoketFile "$base/$asset" $zip
        Get-NoketFile "$base/checksums.txt" $checksums
        Assert-NoketChecksum $zip $checksums $asset

        Expand-Archive -LiteralPath $zip -DestinationPath $tmp -Force
        $srcNoket = Join-Path $tmp 'noket.exe'
        $srcNoketd = Join-Path $tmp 'noketd.exe'
        if (-not (Test-Path -LiteralPath $srcNoket) -or -not (Test-Path -LiteralPath $srcNoketd)) {
            Stop-Installer 'archive did not contain the noket binaries'
        }

        New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
        Copy-Item -LiteralPath $srcNoket -Destination $NoketExe -Force
        Copy-Item -LiteralPath $srcNoketd -Destination $NoketdExe -Force
        Write-Step "Installed to $InstallDir"
    }
    finally {
        Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
    }

    return $tag
}

function Uninstall-Noket {
    Stop-NoketProcesses
    Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false -ErrorAction SilentlyContinue

    Remove-NoketFromUserPath $InstallDir
    Remove-Item -LiteralPath $NoketExe -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $NoketdExe -Force -ErrorAction SilentlyContinue

    if ($Purge) {
        if ($ConfigDir -and $ConfigDir -ne $env:USERPROFILE -and $ConfigDir -ne [IO.Path]::GetPathRoot($ConfigDir)) {
            Remove-Item -LiteralPath $ConfigDir -Recurse -Force -ErrorAction SilentlyContinue
            Write-Step "Removed $ConfigDir"
        }
        else {
            Write-Warn "refusing to remove $ConfigDir; delete it manually if that is what you want"
        }
    }

    Write-Step "noket was uninstalled from $InstallDir"
}

if ($PSVersionTable.PSVersion.Major -lt 5) {
    Stop-Installer 'PowerShell 5.1 or later is required'
}

if ($Uninstall) {
    Uninstall-Noket
    return
}

$tag = Install-Noket
Add-NoketToUserPath $InstallDir
Invoke-NoketLogin
Enable-NoketAutostart

Write-Host ''
Write-Step "noket $tag installed to $InstallDir"
Write-Host ''
Write-Host '  noket login    sign in (if you have not already)'
Write-Host '  noket status   check the tunnel'
Write-Host '  noket tui      manage upstreams'
Write-Host ''
Write-Host 'Open a new terminal so the updated PATH takes effect.'
Write-Host "Logs: $(Join-Path $ConfigDir 'noketd.log')"
if (-not (Test-Path -LiteralPath (Join-Path $ConfigDir 'credentials.json'))) {
    Write-Host ''
    Write-Host 'The agent retries until you run: noket login'
}
