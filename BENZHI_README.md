# BENZHI_README

基于 Go 实现的oral-archive-release HTTP API 项目，一款后端服务，已完整实现语言档案馆口述录音研究开放审查服务，覆盖同意范围计算、问题整改复核、批准、清单冻结、链式凭据签发、审计时间线与公开完整性核验。

## 项目说明
- 项目：benzhi-project-99cdac78-04ad-4ad8-9914-0963aa3f9cd5
- 项目用途：已完整实现语言档案馆口述录音研究开放审查服务，覆盖同意范围计算、问题整改复核、批准、清单冻结、链式凭据签发、审计时间线与公开完整性核验。
- Go 工具链：`golang:1.22`
- 前端工具链：无

## 标准构建、运行和测试命令
进入容器后执行：
cd '/app' && GOTOOLCHAIN=local go build ./...
cd /app && GOTOOLCHAIN=local go run ./cmd/archive-release -selfcheck -addr=127.0.0.1:19081 -data-dir=.benzhi/selfcheck-data
cd '/app' && GOTOOLCHAIN=local go test ./...

## Docker 构建和进入容器
chmod +x build_benzhi_docker.sh
./build_benzhi_docker.sh benzhi-project-99cdac78-04ad-4ad8-9914-0963aa3f9cd5-amd64 linux/amd64
./build_benzhi_docker.sh benzhi-project-99cdac78-04ad-4ad8-9914-0963aa3f9cd5-arm64 linux/arm64
docker run -it benzhi-project-99cdac78-04ad-4ad8-9914-0963aa3f9cd5-amd64:latest

## 题目验证命令
1. 预期退出码 0：`go test ./...`
2. 预期退出码 0：`go run ./cmd/archive-release -selfcheck -addr=127.0.0.1:19081 -data-dir=.benzhi/selfcheck-data`
