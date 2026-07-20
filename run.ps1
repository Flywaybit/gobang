$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$serverDir = Join-Path $root "server"
$logFile = Join-Path $root "server.log"

# 启动服务端，并把日志写入项目根目录 server.log   
Start-Process powershell -WorkingDirectory $serverDir -ArgumentList "-NoExit", "-Command", "go run . *>&1 | Tee-Object -FilePath '$logFile'"

# Web 前端访问：http://127.0.0.1:8889
