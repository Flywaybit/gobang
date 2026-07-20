# 启动服务端
Start-Process powershell -ArgumentList "-NoExit", "-Command", "cd server; go run .\serverproto.go .\common.go"

Start-Sleep -Seconds 1

# 启动客户端1
Start-Process powershell -ArgumentList "-NoExit", "-Command", "cd client; go run .\clientproto.go .\common.go"

# 启动客户端2
Start-Process powershell -ArgumentList "-NoExit", "-Command", "cd client; go run .\clientproto.go .\common.go"      