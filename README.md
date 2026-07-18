# GoHttp 高性能网络请求库 \- 项目说明文档

## 📖 项目简介

基于 **req/v3** 高性能 HTTP 库二次封装的 Go 网络请求工具，专为爬虫、接口请求、易语言 CGO 跨语言调用设计。内置 TLS 指纹伪装、Cookie 管理、代理配置、双向 HTTPS 证书认证、并发安全锁、响应体缓存等全套能力，解决原生 HTTP 库功能单一、无法重复读取响应体、并发数据竞争、指纹封禁等问题。

项目支持编译为 EXE 调试、编译为 DLL 供易语言调用，全程线程安全，适配多协程并发请求场景。

## ✨ 核心特性

- **多浏览器 TLS 指纹伪装**：支持 Chrome/Edge/Firefox/Safari/安卓/iOS/360 浏览器指纹，随机指纹防爬虫检测

- **完整请求能力**：支持 GET/POST 等通用请求、文本/二进制请求体发送、自定义请求头

- **HTTPS 安全适配**：支持自定义根证书、客户端双向证书认证、TLS 握手超时单独配置

- **网络代理支持**：兼容 HTTP/SOCKS5 代理，支持动态开启/关闭代理

- **并发安全机制**：全局 ID 锁、单请求独立读写锁，彻底杜绝多协程数据竞争

- **响应体缓存优化**：解决原生 Body 仅单次读取问题，支持重复获取响应文本、二进制数据

- **精细化超时控制**：支持全局请求超时、TLS 握手超时双重超时配置，防止请求卡死

- **错误统一捕获**：所有请求异常统一收集，可随时获取详细错误信息

- **Cookie 灵活管控**：支持手动开启/关闭 Cookie 容器，适配会话保持/纯净请求场景

- **无自动重定向**：默认关闭自动重定向，支持业务层自主处理跳转逻辑

## 📁 项目结构

```Plain Text
gohttp/
├── go.mod       # 项目依赖模块配置
├── main.go      # 程序入口、测试示例
└── 核心源码文件  # 完整HTTP封装逻辑、工具函数、上下文管理
```

## ⚙️ 环境依赖

- Go 版本：支持 32位/64位 Go（推荐 1\.21\+）

- 核心依赖：`github.com/imroc/req/v3` 高性能 HTTP 库

- 适配场景：Go 原生调试、易语言 CGO 导出 DLL、批量网络请求、爬虫开发

依赖安装命令：

```Plain Text
go mod tidy
```

## 🚀 快速使用示例

### 基础 GET 请求示例

```Plain Text
func main() {
	// 创建HTTP请求上下文（唯一实例ID）
	ctxID := HTTPCreate()
	// 初始化GET请求
	HTTPOpen(ctxID, "GET", "https://bfweb.hk.beanfun.com/game_zone/")

	// 设置自定义请求头
	HTTPSetRequestHeader(ctxID, "Host", "bfweb.hk.beanfun.com")
	HTTPSetRequestHeader(ctxID, "User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/150.0.0.0 Safari/537.36")
	HTTPSetRequestHeader(ctxID, "Accept-Language", "zh-CN,zh;q=0.9")

	// 发送请求
	ok := HTTPSend(ctxID, "")
	if !ok {
		println("请求失败：", HTTPGetError(ctxID))
		return
	}

	// 获取响应结果
	res, _ := HTTPGetResponseBody(ctxID)
	status, _ := HTTPGetResponseStatus(ctxID)
	redirectUrl, _ := HTTPGetResponseLocation(ctxID)

	println("请求状态：", status)
	println("跳转地址：", redirectUrl)
	println("响应内容：", res)

	HTTPRemove(ctxID) // 释放资源
}
```

### 常用功能调用说明

- `HTTPCreate()`：创建独立 HTTP 请求实例（返回唯一上下文ID）

- `HTTPOpen()`：初始化请求方法、请求地址，重置请求状态

- `HTTPSetRequestHeader()`：设置单次请求独立请求头

- `HTTPSend()`：发送文本请求体请求

- `HTTPSendBin()`：发送二进制请求体（文件/字节流）

- `HTTPGetResponseBody()`：获取响应文本内容

- `HTTPGetResponseLocation()`：获取跳转 Location 协议头

- `HTTPGetError()`：获取请求详细错误信息

- `HTTPRemove()`：销毁实例、释放网络资源，杜绝内存泄漏

## 🛠️ 运行 \& 调试教程

### 1\. CMD/PowerShell 运行（无打包）

进入项目根目录执行：

```Plain Text
# 加载依赖（首次运行必执行）
go mod tidy

# 直接运行项目（加载全部源码，无undefined报错）
go run .
```

### 2\. GoLand 调试解决方案（解决32位Go调试报错）

**报错问题**：32位 Go 编译程序，GoLand 自带64位调试器不兼容，提示 `unsupported architecture of windows/i386`

**永久解决办法**：

1. 更换 **64位 Go SDK**，在 GoLand 设置中切换 GOROOT

2. 运行配置修改：**Run kind 改为 Directory**（文件夹模式），不要单文件运行

3. 打断点后点击 Debug 按钮，正常可视化调试

### 3\. 关键避坑点（必看）

- **禁止单文件运行/编译**：不要单独运行 `main.go`，会导致找不到自定义函数，必须运行整个目录

- **请求头与域名统一**：请求 URL 必须和 Host 头域名一致，否则会被服务器拦截、返回空内容

- **必须手动释放资源**：请求结束必须调用 `HTTPRemove`，防止连接池内存堆积

## 📌 高级功能说明

- **TLS指纹伪装**：`HTTPOpenTLSfingerprint(ctxID, 1)` 1=Chrome、2=Edge、3=Firefox、0=随机指纹

- **代理配置**：`HTTPSetProxy(ctxID, "http://127.0.0.1:7890")`，空字符串关闭代理

- **超时配置**：支持自定义请求总超时、TLS 握手超时，避免请求卡死

- **证书认证**：支持自签名证书、双向 HTTPS 认证，适配内网/加密接口

## 📝 常见问题

#### Q：运行后无任何输出、空白结果？

A：大概率是 **URL 与 Host 请求头不匹配** 导致请求被拦截，或网络请求失败；调用 `HTTPGetError()` 打印错误即可定位问题。

#### Q：提示 undefined 未定义函数？

A：使用了单文件运行/编译，未加载完整源码；切换为目录运行模式 `go run .` 即可解决。

## 📄 开源说明

本项目为自研封装 HTTP 工具库，可自由用于个人开发、爬虫项目、易语言二次开发，支持自定义修改拓展功能，无任何使用限制。

> （注：部分内容可能由 AI 生成）
