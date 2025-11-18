# run_services.ps1

# Define all service directories
$services = @(
    "cmd\api_gateway",
    "cmd\auth_service",
    "cmd\order_service",
    "cmd\product_service",
    "cmd\seckill_service",  # Fixed spelling
    "cmd\user_service",
    "cmd\order_consumer",
    "cmd\product_consumer"
)

# Store process information
$processes = @()

foreach ($service in $services) {
    if (Test-Path $service) {
        Write-Host "Starting service: $service" -ForegroundColor Yellow
        $process = Start-Process -FilePath "go" -ArgumentList "run main.go" -WorkingDirectory $service -PassThru -NoNewWindow
        $processes += $process
        Start-Sleep -Milliseconds 500
    } else {
        Write-Host "Warning: Directory $service does not exist" -ForegroundColor Red
    }
}

Write-Host "All services started!" -ForegroundColor Green
Write-Host "Press Ctrl+C to stop all services"

# Wait for user interruption
try {
    while ($true) {
        Start-Sleep -Seconds 1
    }
}
finally {
    Write-Host "Stopping all services..." -ForegroundColor Yellow
    foreach ($process in $processes) {
        if (!$process.HasExited) {
            $process.Kill()
            Write-Host "Stopped process: $($process.Id)" -ForegroundColor Red
        }
    }
    Write-Host "All services stopped" -ForegroundColor Green
}