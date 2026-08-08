param(
    [string]$BaseUrl = "http://localhost:8080",
    [string]$ReportPath = "",
    [switch]$SkipBuild
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$repoRoot = Split-Path -Parent $PSScriptRoot
$goCommand = Get-Command go -ErrorAction Stop
$goExecutable = $goCommand.Source
$binDirectory = Join-Path $repoRoot "bin"
$demoDirectory = Join-Path $repoRoot ".cache\demo"
$env:GOCACHE = Join-Path $repoRoot ".cache\go-build"

if ([string]::IsNullOrWhiteSpace($ReportPath)) {
    $ReportPath = Join-Path $demoDirectory "acceptance-report.json"
} elseif (-not [System.IO.Path]::IsPathRooted($ReportPath)) {
    $ReportPath = Join-Path $repoRoot $ReportPath
}
$ReportPath = [System.IO.Path]::GetFullPath($ReportPath)
$reportDirectory = Split-Path -Parent $ReportPath
$startedAt = [DateTimeOffset]::UtcNow
$gitCommit = (& git -C $repoRoot rev-parse HEAD).Trim()

New-Item -ItemType Directory -Force -Path $binDirectory, $demoDirectory, $reportDirectory | Out-Null

$serviceDefinitions = @(
    @{ Name = "player-service"; Package = ".\cmd\player-service" },
    @{ Name = "matchmaking-service"; Package = ".\cmd\matchmaking-service" },
    @{ Name = "simulation-service"; Package = ".\cmd\simulation-service" },
    @{ Name = "agent-service"; Package = ".\cmd\agent-service" },
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
        $headers = @{ "Idempotency-Key" = "demo-create-$index"; "X-Player-ID" = $playerId }
        $created = Invoke-RestMethod -Method Post -Uri "$BaseUrl/api/v1/tickets" -Headers $headers -ContentType "application/json" -Body $ticketBody
        $tickets += $created.ticket
    }

    $matchId = ""
    for ($attempt = 0; $attempt -lt 120; $attempt++) {
        $ticketHeaders = @{ "X-Player-ID" = "demo-player-00" }
        $ticket = Invoke-RestMethod -Method Get -Uri "$BaseUrl/api/v1/tickets/$($tickets[0].id)" -Headers $ticketHeaders
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
    $analysis = Invoke-RestMethod -Method Get -Uri "$BaseUrl/api/v1/analytics/match-quality?mode=ranked_5v5&server_region=hongkong&limit=10"
    $replayBody = @{ policy_versions = @("v1-greedy", "v2-beam") } | ConvertTo-Json -Depth 3
    $replay = Invoke-RestMethod -Method Post -Uri "$BaseUrl/api/v1/matches/$matchId/replay" -ContentType "application/json" -Body $replayBody
    $agentBody = @{
        base_policy_version = $match.match.policy_version
        mode = "ranked_5v5"
        server_region = "hongkong"
        historical_limit = 10
    } | ConvertTo-Json -Depth 3
    $agentHeaders = @{ "X-Operator-ID" = "demo-analyst"; "X-Operator-Role" = "analyst" }
    $agent = Invoke-RestMethod -Method Post -Uri "$BaseUrl/api/v1/agent/runs" -Headers $agentHeaders -ContentType "application/json" -Body $agentBody

    if ($match.match.state -ne "MATCH_STATE_READY") {
        throw "Expected READY Match before simulation, got $($match.match.state)"
    }
    if ($simulation.random_seed -ne 42) {
        throw "Simulation did not preserve random seed 42"
    }
    if ($rating.history.Count -ne 1) {
        throw "Expected exactly one rating history entry, got $($rating.history.Count)"
    }
    if ($analysis.observations.Count -lt 1 -or $analysis.summaries.Count -lt 1) {
        throw "Quality analysis did not return observations and summaries"
    }
    if ($replay.outcomes.Count -ne 2) {
        throw "Expected two replay outcomes, got $($replay.outcomes.Count)"
    }
    if ($agent.proposal.risk_report.findings.Count -ne 5) {
        throw "Expected five Agent risk findings, got $($agent.proposal.risk_report.findings.Count)"
    }

    $apiMetricsBody = (Invoke-WebRequest -UseBasicParsing -Method Get -Uri "$BaseUrl/metrics" -TimeoutSec 5).Content
    $matchmakingMetricsBody = (Invoke-WebRequest -UseBasicParsing -Method Get -Uri "http://localhost:8082/metrics" -TimeoutSec 5).Content
    if (-not $apiMetricsBody.Contains("api_http_request_total")) {
        throw "API metrics did not expose api_http_request_total"
    }
    if (-not $matchmakingMetricsBody.Contains("match_success_total")) {
        throw "Matchmaking metrics did not expose match_success_total"
    }

    $completedAt = [DateTimeOffset]::UtcNow
    $report = [ordered]@{
        schema_version = 1
        status = "passed"
        git_commit = $gitCommit
        started_at = $startedAt.ToString("o")
        completed_at = $completedAt.ToString("o")
        elapsed_milliseconds = [math]::Round(($completedAt - $startedAt).TotalMilliseconds)
        services = @($serviceDefinitions | ForEach-Object { $_.Name })
        flow = [ordered]@{
            players_created = 10
            tickets_created = $tickets.Count
            match_id = $matchId
            match_state_before_simulation = $match.match.state
            predicted_win_rate_a = $match.match.predicted_win_rate_a
            quality_score = $match.match.quality_score
            winning_team = $simulation.winning_team
            random_seed = $simulation.random_seed
            player_00_rating_after_match = $rating.rating
            player_00_history_entries = $rating.history.Count
            quality_prediction_absolute_error = $analysis.observations[0].absolute_quality_error
            policy_quality_summary_count = $analysis.summaries.Count
            replay_outcomes = @($replay.outcomes | ForEach-Object {
                [ordered]@{ policy_version = $_.policy_version; quality_score = $_.quality.total_score }
            })
            agent_run_id = $agent.run.id
            agent_proposal_id = $agent.proposal.id
            agent_risk_passed = $agent.proposal.risk_report.passed
            agent_risk_finding_count = $agent.proposal.risk_report.findings.Count
        }
        checks = [ordered]@{
            api_ready = $true
            match_created = $true
            deterministic_seed_preserved = $true
            rating_history_persisted = $true
            quality_analysis_returned = $true
            greedy_and_beam_replayed = $true
            five_agent_risks_returned = $true
            api_metrics_exposed = $true
            matchmaking_metrics_exposed = $true
        }
    }
    $report | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $ReportPath -Encoding UTF8

    [pscustomobject]@{
        status = $report.status
        git_commit = $report.git_commit
        elapsed_milliseconds = $report.elapsed_milliseconds
        match_id = $report.flow.match_id
        quality_score = $report.flow.quality_score
        winning_team = $report.flow.winning_team
        rating_history_entries = $report.flow.player_00_history_entries
        replay_policy_versions = (($report.flow.replay_outcomes | ForEach-Object { $_.policy_version }) -join ", ")
        agent_risk_finding_count = $report.flow.agent_risk_finding_count
        report_path = $ReportPath
    } | Format-List
} catch {
    $completedAt = [DateTimeOffset]::UtcNow
    $logTails = [ordered]@{}
    foreach ($service in $serviceDefinitions) {
        $stderr = Join-Path $demoDirectory ($service.Name + ".stderr.log")
        if (Test-Path -LiteralPath $stderr) {
            $logTails[$service.Name] = @((Get-Content -LiteralPath $stderr -Tail 20 -ErrorAction SilentlyContinue))
        }
    }
    [ordered]@{
        schema_version = 1
        status = "failed"
        git_commit = $gitCommit
        started_at = $startedAt.ToString("o")
        completed_at = $completedAt.ToString("o")
        elapsed_milliseconds = [math]::Round(($completedAt - $startedAt).TotalMilliseconds)
        error = $_.Exception.Message
        stderr_tails = $logTails
    } | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $ReportPath -Encoding UTF8
    throw
} finally {
    foreach ($process in $processes) {
        if (-not $process.HasExited) {
            Stop-Process -Id $process.Id -Force
            Wait-Process -Id $process.Id -Timeout 5 -ErrorAction SilentlyContinue
        }
    }
}
