param(
    [string]$BaseUrl = "http://localhost:8080",
    [switch]$SkipBuild
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$repoRoot = Split-Path -Parent $PSScriptRoot
$goExecutable = "C:\Program Files\Go\bin\go.exe"
$binDirectory = Join-Path $repoRoot "bin"
$demoDirectory = Join-Path $repoRoot ".cache\demo"
$env:GOCACHE = Join-Path $repoRoot ".cache\go-build"

New-Item -ItemType Directory -Force -Path $binDirectory, $demoDirectory | Out-Null

$serviceDefinitions = @(
    @{ Name = "player-service"; Package = ".\cmd\player-service" },
    @{ Name = "matchmaking-service"; Package = ".\cmd\matchmaking-service" },
    @{ Name = "simulation-service"; Package = ".\cmd\simulation-service" },
    @{ Name = "api-service"; Package = ".\cmd\api-service" }
)

if (-not $SkipBuild) {
    foreach ($service in $serviceDefinitions) {
        $executable = Join-Path $binDirectory ($service.Name + ".exe")
        Write-Host "Building $($service.Name)..."
        & $goExecutable build -o $executable $service.Package
        if ($LASTEXITCODE -ne 0) {
            throw "Failed to build $($service.Name)"
        }
    }
}

$processes = @()
try {
    foreach ($service in $serviceDefinitions) {
        $executable = Join-Path $binDirectory ($service.Name + ".exe")
        if (-not (Test-Path -LiteralPath $executable)) {
            throw "Missing executable: $executable"
        }
        $stdout = Join-Path $demoDirectory ($service.Name + ".stdout.log")
        $stderr = Join-Path $demoDirectory ($service.Name + ".stderr.log")
        $process = Start-Process -FilePath $executable -WorkingDirectory $repoRoot -WindowStyle Hidden -RedirectStandardOutput $stdout -RedirectStandardError $stderr -PassThru
        $processes += $process
        Start-Sleep -Milliseconds 250
    }

    $ready = $false
    for ($attempt = 0; $attempt -lt 120; $attempt++) {
        try {
            $status = Invoke-RestMethod -Method Get -Uri "$BaseUrl/ready" -TimeoutSec 2
            if ($status.status -eq "ready") {
                $ready = $true
                break
            }
        } catch {
            Start-Sleep -Milliseconds 500
        }
    }
    if (-not $ready) {
        throw "MatchMind did not become ready. Inspect $demoDirectory"
    }

    $roles = @("vanguard", "roamer", "core", "ranged", "support")
    $tickets = @()
    for ($index = 0; $index -lt 10; $index++) {
        $playerId = "demo-player-{0:d2}" -f $index
        $role = $roles[$index % $roles.Count]
        $playerBody = @{
            id = $playerId
            name = "Demo Player $index"
            initial_rating = 1500 + (($index % 2) * 10)
            preferred_roles = @($role)
            home_region = "hongkong"
            region_latency = @{ hongkong = 30; singapore = 55 }
            behavior_score = 95
        } | ConvertTo-Json -Depth 5
        Invoke-RestMethod -Method Post -Uri "$BaseUrl/api/v1/players" -ContentType "application/json" -Body $playerBody | Out-Null

        $ticketBody = @{
            player_id = $playerId
            mode = "ranked_5v5"
            client_version = "1.0.0"
            preferred_roles = @($role)
            region_latency = @{ hongkong = 30; singapore = 55 }
        } | ConvertTo-Json -Depth 5
        $headers = @{ "Idempotency-Key" = "demo-create-$index" }
        $created = Invoke-RestMethod -Method Post -Uri "$BaseUrl/api/v1/tickets" -Headers $headers -ContentType "application/json" -Body $ticketBody
        $tickets += $created.ticket
    }

    $matchId = ""
    for ($attempt = 0; $attempt -lt 120; $attempt++) {
        $ticket = Invoke-RestMethod -Method Get -Uri "$BaseUrl/api/v1/tickets/$($tickets[0].id)"
        if ($ticket.ticket.state -eq "TICKET_STATE_ASSIGNED") {
            $matchId = $ticket.ticket.match_id
            break
        }
        Start-Sleep -Milliseconds 250
    }
    if ([string]::IsNullOrWhiteSpace($matchId)) {
        throw "The ten demo tickets were not assigned to a match"
    }

    $match = Invoke-RestMethod -Method Get -Uri "$BaseUrl/api/v1/matches/$matchId"
    $simulation = Invoke-RestMethod -Method Post -Uri "$BaseUrl/api/v1/matches/$matchId/simulate" -ContentType "application/json" -Body '{"random_seed":42}'
    $rating = Invoke-RestMethod -Method Get -Uri "$BaseUrl/api/v1/players/demo-player-00/rating"

    [pscustomobject]@{
        match_id = $matchId
        match_state_before_simulation = $match.match.state
        predicted_win_rate_a = $match.match.predicted_win_rate_a
        quality_score = $match.match.quality_score
        winning_team = $simulation.winning_team
        random_seed = $simulation.random_seed
        player_00_rating_after_match = $rating.rating
        player_00_history_entries = $rating.history.Count
        api_metrics = "$BaseUrl/metrics"
        matchmaking_metrics = "http://localhost:8082/metrics"
    } | Format-List
} finally {
    foreach ($process in $processes) {
        if (-not $process.HasExited) {
            Stop-Process -Id $process.Id -Force
        }
    }
}
