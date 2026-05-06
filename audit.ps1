<#
.SYNOPSIS
    Audits a projects folder for files older than a specified age.

.DESCRIPTION
    Scans a projects folder (and optionally an "Archived Projects" subfolder),
    finds all files older than the specified cutoff date, and exports the results
    to a CSV report.

    Owner is resolved via Windows ACL; falls back to "Unknown (ACL unavailable)" if unavailable.

.PARAMETER Path
    Path to the root projects folder to audit.

.PARAMETER Age
    Age cutoff in the format <number><unit>, where unit is:
      d = days, w = weeks, m = months, y = years
    Examples: 2y, 6m, 90d, 3w
    Default: 2y

.PARAMETER FilterBy
    Which date field to use when applying the age cutoff filter:
      Created     - filter on file creation date only
      Modified    - filter on last modified date only
      Oldest      - filter on whichever of the two dates is older (default)

.PARAMETER OutputPath
    Path for the output CSV file.
    Default: .\ProjectAudit_<timestamp>.csv
.PARAMETER OutputFormat
    Format for the output file: CSV (default) or Confluence.
    Default: Confluence

.EXAMPLE
    .\Invoke-ProjectAudit.ps1 -Path "C:\Projects"

    Basic audit using all defaults: 2-year cutoff, oldest date, Confluence wiki format, timestamped in current directory.

.EXAMPLE
    .\Invoke-ProjectAudit.ps1 -Path "C:\Projects" -Age 6m -OutputPath "C:\Reports\audit.csv" -OutputFormat CSV

    Audit files older than 6 months (oldest date), saved to a specific path.

.EXAMPLE
    .\Invoke-ProjectAudit.ps1 -Path "C:\Projects" -Age 2y -FilterBy Created

    Audit files whose creation date is more than 2 years ago.

.EXAMPLE
    .\Invoke-ProjectAudit.ps1 -Path "C:\Projects" -Age 1y -FilterBy Modified -OutputPath "C:\Reports\modified_audit.csv"

    Audit files that haven't been modified in over a year.
#>

[CmdletBinding()]
param (
    [Parameter(Mandatory = $true, HelpMessage = "Path to the root projects folder.")]
    [ValidateScript({ Test-Path $_ -PathType Container })]
    [string]$Path,

    [Parameter(Mandatory = $false, HelpMessage = "Age cutoff e.g. 2y, 6m, 90d, 3w. Default: 2y")]
    [ValidatePattern('^\d+[dwmy]$')]
    [string]$Age = "2y",

    [Parameter(Mandatory = $false, HelpMessage = "Date field to filter on: Created, Modified, or Oldest (default).")]
    [ValidateSet("Created", "Modified", "Oldest")]
    [string]$FilterBy = "Oldest",

    [Parameter(Mandatory = $false, HelpMessage = "Output CSV file path.")]
    [string]$OutputPath = ".\ProjectAudit_$(Get-Date -Format 'yyyyMMdd_HHmmss').csv",

    [Parameter(Mandatory = $false, HelpMessage = "Output format: CSV (default) or Confluence.")]
    [ValidateSet("CSV", "Confluence")]
    [string]$OutputFormat = "Confluence"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Continue"

# ---------------------------------------------------------------------------
# Helper: Parse age string into a cutoff DateTime
# ---------------------------------------------------------------------------
function Get-CutoffDate {
    param ([string]$AgeString)

    $value = [int]($AgeString -replace '[dwmy]', '')
    $unit  = $AgeString[-1]

    switch ($unit) {
        'd' { return (Get-Date).AddDays(-$value) }
        'w' { return (Get-Date).AddDays(-($value * 7)) }
        'm' { return (Get-Date).AddMonths(-$value) }
        'y' { return (Get-Date).AddYears(-$value) }
    }
}

# ---------------------------------------------------------------------------
# Helper: Format a TimeSpan into a human-readable age string e.g. "2y 6m 3d"
# ---------------------------------------------------------------------------
function Format-Age {
    param ([TimeSpan]$Span)

    $totalDays = [int]$Span.TotalDays
    if ($totalDays -le 0) { return "0d" }

    $years  = [math]::Floor($totalDays / 365)
    $remain = $totalDays - ($years * 365)
    $months = [math]::Floor($remain / 30)
    $days   = $remain - ($months * 30)

    $parts = @()
    if ($years  -gt 0) { $parts += "${years}y"  }
    if ($months -gt 0) { $parts += "${months}m" }
    if ($days   -gt 0) { $parts += "${days}d"   }

    if ($parts.Count -eq 0) { return "0d" }
    return $parts -join " "
}

# ---------------------------------------------------------------------------
# Helper: Format bytes into human-readable size (KB, MB, GB, etc.)
# ---------------------------------------------------------------------------
function Format-Size {
    param ([long]$Bytes)

    if ($Bytes -lt 1KB) { return "$Bytes B" }
    elseif ($Bytes -lt 1MB) { return "{0:N1} KB" -f ($Bytes / 1KB) }
    elseif ($Bytes -lt 1GB) { return "{0:N1} MB" -f ($Bytes / 1MB) }
    elseif ($Bytes -lt 1TB) { return "{0:N1} GB" -f ($Bytes / 1GB) }
    else { return "{0:N1} TB" -f ($Bytes / 1TB) }
}

# ---------------------------------------------------------------------------
# Helper: Resolve file owner via ACL, fallback if unavailable
# ---------------------------------------------------------------------------
function Get-FileOwner {
    param ([System.IO.FileInfo]$File)

    try {
        $acl = Get-Acl -LiteralPath $File.FullName -ErrorAction Stop
        if ($acl.Owner) { return $acl.Owner }
    }
    catch { }

    return "Unknown (ACL unavailable)"
}

# ---------------------------------------------------------------------------
# Helper: Select the date to filter on based on the -FilterBy parameter
# ---------------------------------------------------------------------------
function Get-FilterDate {
    param (
        [System.IO.FileInfo]$File,
        [string]$Mode
    )

    switch ($Mode) {
        "Created"  { return $File.CreationTime }
        "Modified" { return $File.LastWriteTime }
        "Oldest"   {
            if ($File.CreationTime -lt $File.LastWriteTime) {
                return $File.CreationTime
            } else {
                return $File.LastWriteTime
            }
        }
    }
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

$cutoffDate = Get-CutoffDate -AgeString $Age
$rootPath   = (Resolve-Path $Path).Path

$filterByLabel = switch ($FilterBy) {
    "Created"  { "Created date" }
    "Modified" { "Last modified date" }
    "Oldest"   { "Oldest of created / last modified" }
}

Write-Host ""
Write-Host "  Project Audit" -ForegroundColor Cyan
Write-Host "  -----------------------------------------" -ForegroundColor DarkGray
Write-Host "  Root path   : $rootPath"
Write-Host "  Age cutoff  : $Age (before $($cutoffDate.ToString('yyyy-MM-dd')))"
Write-Host "  Filter by   : $filterByLabel"
Write-Host "  Output file : $OutputPath"
Write-Host "  Output type : $OutputFormat"
Write-Host "  -----------------------------------------" -ForegroundColor DarkGray
Write-Host ""

# ---------------------------------------------------------------------------
# Build the list of project root folders to scan
# ---------------------------------------------------------------------------
$projectRoots = [System.Collections.Generic.List[hashtable]]::new()

$topLevel = Get-ChildItem -LiteralPath $rootPath -Directory -ErrorAction SilentlyContinue

foreach ($dir in $topLevel) {
    if ($dir.Name -ieq "Archived Projects") {
        $archived = Get-ChildItem -LiteralPath $dir.FullName -Directory -ErrorAction SilentlyContinue
        foreach ($sub in $archived) {
            $projectRoots.Add(@{ Folder = $sub.Name; Path = $sub.FullName; Archived = $true })
        }
    }
    else {
        $projectRoots.Add(@{ Folder = $dir.Name; Path = $dir.FullName; Archived = $false })
    }
}

if ($projectRoots.Count -eq 0) {
    Write-Warning "No project folders found under: $rootPath"
    exit 1
}

Write-Host "  Found $($projectRoots.Count) project folder(s) to scan." -ForegroundColor Green
Write-Host ""

# ---------------------------------------------------------------------------
# Collect all files across all project folders
# ---------------------------------------------------------------------------
Write-Progress -Activity "Project Audit" -Status "Discovering files..." -PercentComplete 0

$allFiles       = [System.Collections.Generic.List[System.IO.FileInfo]]::new()
$fileProjectMap = @{}   # FileInfo.FullName -> project folder label

$projIndex = 0
foreach ($proj in $projectRoots) {
    $projIndex++
    $pct = [int](($projIndex / $projectRoots.Count) * 30)   # Discovery = first 30%
    Write-Progress -Activity "Project Audit" `
                   -Status "Discovering: $($proj.Folder)" `
                   -PercentComplete $pct

    $files = Get-ChildItem -LiteralPath $proj.Path -File -Recurse -ErrorAction SilentlyContinue
    foreach ($f in $files) {
        $allFiles.Add($f)
        $label = if ($proj.Archived) { "[Archived] $($proj.Folder)" } else { $proj.Folder }
        $fileProjectMap[$f.FullName] = $label
    }
}

Write-Host "  Total files discovered : $($allFiles.Count)" -ForegroundColor DarkCyan

# ---------------------------------------------------------------------------
# Filter files by age cutoff using the selected date field
# ---------------------------------------------------------------------------
$filteredFiles = @(
    $allFiles | Where-Object {
        (Get-FilterDate -File $_ -Mode $FilterBy) -lt $cutoffDate
    }
)

Write-Host "  Files matching cutoff  : $($filteredFiles.Count)" -ForegroundColor DarkCyan
Write-Host ""

if ($filteredFiles.Count -eq 0) {
    Write-Warning "No files matching the '$FilterBy' filter older than '$Age' were found."
    Write-Progress -Activity "Project Audit" -Completed
    exit 0
}

# ---------------------------------------------------------------------------
# Build report rows
# ---------------------------------------------------------------------------
$results = [System.Collections.Generic.List[PSCustomObject]]::new()
$total   = $filteredFiles.Count
$index   = 0
$today   = Get-Date

foreach ($file in $filteredFiles) {
    $index++
    $pct = 30 + [int](($index / $total) * 65)   # Processing = 30-95%

    Write-Progress -Activity "Project Audit" `
                   -Status "Processing file $index of $total" `
                   -CurrentOperation $file.Name `
                   -PercentComplete $pct

    $owner        = Get-FileOwner -File $file
    $created      = $file.CreationTime
    $lastModified = $file.LastWriteTime
    $oldest       = if ($created -lt $lastModified) { $created } else { $lastModified }
    $ageSpan      = $today - $oldest
    $modSpan      = $today - $lastModified
    $projectLabel = $fileProjectMap[$file.FullName]

    $results.Add([PSCustomObject]@{
        FileName          = $file.Name
        FullPath          = $file.FullName
        ProjectFolder     = $projectLabel
        Owner             = $owner
        Size         = Format-Size -Bytes $file.Length
        Created           = $created.ToString('yyyy-MM-dd HH:mm:ss')
        LastModified      = $lastModified.ToString('yyyy-MM-dd HH:mm:ss')
        Age               = Format-Age -Span $ageSpan
        DaysSinceModified = Format-Age -Span $modSpan
    })
}

# ---------------------------------------------------------------------------
# Export results
# ---------------------------------------------------------------------------
Write-Progress -Activity "Project Audit" -Status "Writing output..." -PercentComplete 96

try {
    if ($OutputFormat -eq "CSV") {
        $results | Export-Csv -LiteralPath $OutputPath -NoTypeInformation -Encoding UTF8
    }
    elseif ($OutputFormat -eq "Confluence") {

        # Build Confluence wiki table
        $headers = @(
            "FileName","FullPath","ProjectFolder","Owner","Size",
            "Created","LastModified","Age","DaysSinceModified",
            "ActionRequired","Notes"
        )

        $lines = [System.Collections.Generic.List[string]]::new()

        # Header row
        $lines.Add("||" + ($headers -join "||") + "||")

        # Data rows
        foreach ($row in $results) {
            $values = foreach ($h in $headers) {
                switch ($h) {
                    "ActionRequired" { "" }
                    "Notes"          { "" }
                    default          { ($row.$h -replace '\|','\\|') }
                }
            }
            $lines.Add("|" + ($values -join "|") + "|")
        }

        # Ensure file extension makes sense
        if (-not $OutputPath.EndsWith(".txt")) {
            $OutputPath = [System.IO.Path]::ChangeExtension($OutputPath, ".txt")
        }

        $lines | Set-Content -LiteralPath $OutputPath -Encoding UTF8
    }

    Write-Progress -Activity "Project Audit" -Completed

    Write-Host "  -----------------------------------------" -ForegroundColor DarkGray
    Write-Host "  Audit complete." -ForegroundColor Green
    Write-Host "  Files reported : $($results.Count)"
    Write-Host "  Output saved   : $OutputPath" -ForegroundColor Cyan
    Write-Host "  -----------------------------------------" -ForegroundColor DarkGray
    Write-Host ""
}
catch {
    Write-Progress -Activity "Project Audit" -Completed
    Write-Error "Failed to write output to '$OutputPath': $_"
    exit 1
}