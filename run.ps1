$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$serverDir = Join-Path $root "server"

# 启动服务端，日志由 Go 服务写入项目根目录 log/server.log 和 log/game.log
Start-Process powershell -WorkingDirectory $serverDir -ArgumentList "-NoExit", "-Command", "[Console]::OutputEncoding=[System.Text.Encoding]::UTF8; chcp 65001 > `$null; go run ."

# Web 前端访问：http://127.0.0.1:8889
