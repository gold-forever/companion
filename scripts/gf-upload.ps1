# Uploads every GoldForever.lua under a World of Warcraft folder to Gold Forever each time the game
# writes it (a /reload or a logout). The whole protocol, for anyone who would rather not run the app.
#
#   $env:GF_TOKEN = "gf_..."
#   .\gf-upload.ps1 -Wow "C:\Program Files (x86)\World of Warcraft"
#
# Windows PowerShell 5.1 or PowerShell 7. A downloaded script is blocked until you run
# Unblock-File .\gf-upload.ps1 (see the README). Stop it with Ctrl-C.
param(
    [Parameter(Mandatory = $true)][string]$Wow,
    [string]$Url = "https://gold-forever.app"
)
$ErrorActionPreference = "Stop"
if (-not $env:GF_TOKEN) { throw "Set GF_TOKEN to a token from /me/tokens" }

# The token travels in a header object, never on a command line.
$headers = @{ Authorization = "Bearer $($env:GF_TOKEN)" }
$seen = @{}; $since = @{}; $sent = @{}

function Test-Complete([string]$Path) {
    # The game writes the file in more than one go: a complete one ends with the closing brace.
    $stream = [System.IO.File]::Open($Path, 'Open', 'Read', 'ReadWrite')
    try {
        $tail = New-Object byte[] ([Math]::Min(64, $stream.Length))
        [void]$stream.Seek(-$tail.Length, 'End'); [void]$stream.Read($tail, 0, $tail.Length)
        return ([Text.Encoding]::ASCII.GetString($tail).Trim()).EndsWith("}")
    } finally { $stream.Dispose() }
}

function Send-Save([string]$Path, [string]$Site) {
    try {
        $answer = Invoke-WebRequest -UseBasicParsing -Method Post -Headers $headers -InFile $Path `
            -ContentType "application/octet-stream" -UserAgent "gf-upload.ps1" `
            -Uri "$Site/api/v1/saves?name=GoldForever.lua"
        return "$($answer.Content) (HTTP $($answer.StatusCode))"
    } catch {
        return "failed: $($_.Exception.Message)"
    }
}

Write-Output "watching $Wow every 2 s; Ctrl-C stops"
while ($true) {
    $files = Get-ChildItem -Path (Join-Path $Wow "_*_/WTF/Account/*/SavedVariables/GoldForever.lua") -ErrorAction SilentlyContinue
    foreach ($file in $files) {
        $path = $file.FullName
        $now = "$($file.Length) $($file.LastWriteTimeUtc.Ticks)"
        if ($seen[$path] -ne $now) {
            # Changed since the last look: wait until it has stood still for 2 s.
            $seen[$path] = $now; $since[$path] = Get-Date; continue
        }
        if (((Get-Date) - $since[$path]).TotalSeconds -lt 2 -or $sent[$path] -eq $now) { continue }
        if (-not (Test-Complete $path)) { continue }
        $sent[$path] = $now
        # Only the local log names the folder; the upload carries the file's bytes and nothing else.
        Write-Output "$(Get-Date -Format HH:mm:ss) $($path.Substring($Wow.Length).TrimStart('\', '/')): $(Send-Save $path $Url)"
    }
    Start-Sleep -Seconds 2
}
