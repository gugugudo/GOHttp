// 包声明：标准main包，可编译为可执行程序，可拓展CGO导出为DLL/SO
package main

import "C"
import (
	"bytes"      // 字节缓冲区，用于读取、缓存HTTP响应二进制流
	"crypto/tls" // TLS证书工具，支持客户端双向HTTPS认证
	"io"         // IO流操作，处理HTTP响应只读流
	"net/http/cookiejar"
	"strings" // 字符串分割、清洗，解析自定义请求头
	"sync"    // 并发锁，解决多线程竞争、保证线程安全
	"time"    // 时间类型，用于HTTP超时配置
	// 高性能HTTP第三方库：req/v3
	// 支持指纹伪装、HTTP1.1/2、代理、证书配置、链式调用，稳定性极强
	"github.com/imroc/req/v3"
)

// ========================== 全局上下文ID生成器 ==========================

// MessageIdLock ID生成全局互斥锁，保证多协程下ID不重复
var MessageIdLock sync.Mutex //

// messageId 上下文ID自增初始值，用于生成唯一HTTP请求上下文标识
var messageId = 1000

// newMessageId 生成全局唯一的上下文ID
// 作用：为每一个HTTP实例分配独立ID，实现多请求隔离
// 容错机制：ID溢出后自动重置，防止int数值越界错乱
func newMessageId() int {
	// 加锁保证并发安全
	MessageIdLock.Lock()
	defer MessageIdLock.Unlock()

	// ID自增
	messageId++
	currentID := messageId

	// int32最大值边界保护，防止数值溢出
	if currentID < 0 || currentID > 2147483640 {
		currentID = 9999
		messageId = 1000
	}
	return currentID
}

// ========================== HTTP上下文结构体（核心优化） ==========================

// HTTPContext HTTP请求上下文结构体
// 优化点：新增BodyBuf缓存响应体、单实例独立锁、资源完整管控、字段语义清晰
// 单个上下文独立隔离：代理、指纹、请求头、响应数据互不干扰
type HTTPContext struct {
	Client   *req.Client   // req客户端实例，承载全局配置（代理、指纹、超时、全局请求头）
	Request  *req.Request  // 单次HTTP请求实例，仅当前请求生效
	URL      string        // 当前请求目标地址
	Method   string        // 当前请求请求方法（GET/POST/PUT/DELETE等）
	Response *req.Response // 请求响应原始对象
	BodyBuf  []byte        // 【核心优化】缓存响应二进制数据，解决原生Body仅可读一次的问题
	Context  int           // 当前上下文唯一ID
	Error    string        // 当前请求错误信息，用于上层获取异常详情
	Lock     sync.RWMutex  // 单上下文独立锁，杜绝多线程并发修改数据竞争
}

// ========================== 全局上下文管理器 ==========================

// HTTPMap 全局上下文注册表，存储所有活跃HTTP上下文 {上下文ID:上下文实例}
var HTTPMap = make(map[int]*HTTPContext)

// HTTPMapLock 全局注册表读写锁，保证多协程增删查上下文安全
var HTTPMapLock sync.Mutex

// LoadHTTPContext 根据上下文ID加载对应HTTP实例
// 线程安全：全局加锁读取，避免并发读写map报错
func LoadHTTPContext(ctxID int) *HTTPContext {
	HTTPMapLock.Lock()
	context := HTTPMap[ctxID]
	HTTPMapLock.Unlock()
	return context
}

// ========================== 核心实例创建/销毁 ==========================

// HTTPCreate 创建全新的HTTP客户端实例
// 返回值：唯一上下文ID，所有操作均通过该ID绑定实例
// 默认配置：关闭自动重定向、关闭默认Cookie存储，纯净请求环境
func HTTPCreate() int {
	// 生成唯一上下文ID
	ctxID := newMessageId()

	// 初始化上下文结构体
	hc := &HTTPContext{}
	// 创建req原生客户端
	hc.Client = req.C()
	// 绑定上下文ID
	hc.Context = ctxID

	// 禁止自动重定向，由业务层自主处理跳转逻辑
	hc.Client.SetRedirectPolicy(req.NoRedirectPolicy())
	// 默认关闭Cookie容器，不自动持久化Cookie
	hc.Client.SetCookieJar(nil)

	// 注册到全局上下文表
	HTTPMapLock.Lock()
	HTTPMap[ctxID] = hc
	HTTPMapLock.Unlock()

	return ctxID
}

// HTTPRemove 销毁HTTP实例，释放所有资源
// 优化点：主动关闭空闲连接，彻底释放连接池资源，杜绝内存堆积
func HTTPRemove(ctxID int) {
	HTTPMapLock.Lock()
	defer HTTPMapLock.Unlock()

	// 关闭客户端空闲连接，释放网络资源
	hc, ok := HTTPMap[ctxID]
	if ok && hc.Client != nil {
		hc.Client.CloseIdleConnections()
	}

	delete(HTTPMap, ctxID)

}

// ========================== 客户端全局配置接口 ==========================

// HTTPOpenTLSfingerprint 设置TLS指纹伪装，绕过网站爬虫检测、设备校验
// mode参数说明：
// 0=随机指纹 1=Chrome 2=Edge 3=Firefox 4=Safari 其他=360浏览器
// 全程加锁，并发切换指纹安全
func HTTPOpenTLSfingerprint(ctxID int, mode int) bool {
	hc := LoadHTTPContext(ctxID)
	if hc == nil {
		return false
	}
	// 单实例加锁，防止并发修改客户端配置
	hc.Lock.Lock()
	defer hc.Lock.Unlock()

	// 根据模式设置对应浏览器指纹
	switch mode {
	case 0:
		hc.Client = hc.Client.SetTLSFingerprintRandomized()
	case 1:
		hc.Client = hc.Client.SetTLSFingerprintChrome()
	case 2:
		hc.Client = hc.Client.SetTLSFingerprintEdge()
	case 3:
		hc.Client = hc.Client.SetTLSFingerprintFirefox()
	case 4:
		hc.Client = hc.Client.SetTLSFingerprintSafari()
	case 5:
		hc.Client = hc.Client.SetTLSFingerprintAndroid()
	case 6:
		hc.Client = hc.Client.SetTLSFingerprintIOS()

	default:
		hc.Client = hc.Client.SetTLSFingerprint360()
	}
	return true
}

// HTTPSetCookieJar 开启/关闭Cookie容器
func HTTPSetCookieJar(ctxID int, isOpen bool) bool {
	hc := LoadHTTPContext(ctxID)
	if hc == nil {
		return false
	}
	hc.Lock.Lock()
	defer hc.Lock.Unlock()

	if isOpen {
		jar, err := cookiejar.New(nil)
		if err != nil {
			return false
		}
		hc.Client = hc.Client.SetCookieJar(jar)
	} else {
		hc.Client = hc.Client.SetCookieJar(nil)
	}
	return true
}

// HTTPSetRootCert 配置自定义根证书（PEM文本格式）
// 适用场景：自签名HTTPS证书、内网私有证书校验
func HTTPSetRootCert(ctxID int, certPem string) bool {
	hc := LoadHTTPContext(ctxID)
	if hc == nil {
		return false
	}
	hc.Lock.Lock()
	defer hc.Lock.Unlock()

	// 加载根证书到客户端
	hc.Client = hc.Client.SetRootCertFromString(certPem)
	return true
}

// HTTPSetCert 配置客户端双向HTTPS认证证书
// pemData：证书二进制数据 keyData：私钥二进制数据
// 失败会自动写入错误信息，上层可通过HTTPGetError获取
func HTTPSetCert(ctxID int, pemData, keyData []byte) bool {
	hc := LoadHTTPContext(ctxID)
	if hc == nil {
		return false
	}
	hc.Lock.Lock()
	defer hc.Lock.Unlock()

	// 解析证书+私钥密钥对
	cert, err := tls.X509KeyPair(pemData, keyData)
	if err != nil {
		hc.Error = err.Error()
		return false
	}
	// 绑定双向认证证书到客户端
	hc.Client = hc.Client.SetCerts(cert)
	return true
}

// HTTPSetTimeout 设置请求超时时间与TLS握手超时时间（单位：毫秒）
// timeoutMs：整体请求总超时 tlstimeoutMs：TLS加密握手单独超时
func HTTPSetTimeout(ctxID int, timeoutMs, tlsTimeoutMs int) bool {
	hc := LoadHTTPContext(ctxID)
	if hc == nil {
		return false
	}
	hc.Lock.Lock()
	defer hc.Lock.Unlock()

	// 设置请求全流程超时
	hc.Client = hc.Client.SetTimeout(time.Duration(timeoutMs) * time.Millisecond)
	// 设置TLS握手阶段超时，防止卡死在加密连接阶段
	hc.Client = hc.Client.SetTLSHandshakeTimeout(time.Duration(tlsTimeoutMs) * time.Millisecond)
	return true
}

// HTTPSetProxy 设置网络代理，支持HTTP/SOCKS5代理
// proxyURL为空字符串时，清空并关闭代理
func HTTPSetProxy(ctxID int, proxyURL string) bool {
	hc := LoadHTTPContext(ctxID)
	if hc == nil {
		return false
	}
	hc.Lock.Lock()
	defer hc.Lock.Unlock()

	newCli := hc.Client.Clone()
	if proxyURL == "" {
		newCli = newCli.SetProxy(nil)
	} else {
		newCli = newCli.SetProxyURL(proxyURL)
	}
	// 统一替换，清晰规范
	hc.Client = newCli
	return true
}

// HTTPSetHeaders 批量设置客户端全局公共请求头
// headersText：多行字符串，换行分隔，格式 key:value
// 全局头：所有通过该实例发起的请求都会默认携带
func HTTPSetHeaders(ctxID int, headersText string) bool {
	hc := LoadHTTPContext(ctxID)
	if hc == nil {
		return false
	}
	hc.Lock.Lock()
	defer hc.Lock.Unlock()

	// 解析多行请求头为map
	headerMap := parseHeaders(headersText)
	// 设置非标准化请求头（保留大小写，不自动规范，适配特殊接口）
	hc.Client = hc.Client.SetCommonHeadersNonCanonical(headerMap)
	return true
}

// HTTPSetHeader 设置单个客户端全局请求头
// key：请求头键名 value：请求头值，空值则删除该请求头
func HTTPSetHeader(ctxID int, key, value string) bool {
	hc := LoadHTTPContext(ctxID)
	if hc == nil {
		return false
	}
	hc.Lock.Lock()
	defer hc.Lock.Unlock()

	if value == "" {
		// 值为空，删除指定全局请求头
		hc.Client.Headers.Del(key)
	} else {
		// 设置/覆盖全局请求头
		hc.Client = hc.Client.SetCommonHeaderNonCanonical(key, value)
	}
	return true
}

// ========================== 单次请求配置接口 ==========================

// HTTPOpen 初始化单次HTTP请求
// method：请求方法 GET/POST/PUT/DELETE
// url：目标请求地址
// 功能：重置单次请求状态、清空旧数据、清空错误信息，保证请求纯净
func HTTPOpen(ctxID int, method, url string) bool {
	hc := LoadHTTPContext(ctxID)
	if hc == nil {
		return false
	}
	hc.Lock.Lock()
	defer hc.Lock.Unlock()

	// 创建全新单次请求实例
	hc.Request = hc.Client.R()
	// 绑定请求参数
	hc.Method = method
	hc.URL = url
	// 重置旧请求数据
	hc.Response = nil
	hc.BodyBuf = nil
	hc.Error = ""
	return true
}

// HTTPSetRequestHeader 设置单次请求独立请求头（仅当前请求生效，不影响全局）
func HTTPSetRequestHeader(ctxID int, key, value string) bool {
	hc := LoadHTTPContext(ctxID)
	if hc == nil {
		return false
	}
	hc.Lock.Lock()
	defer hc.Lock.Unlock()

	// 未初始化请求，返回错误
	if hc.Request == nil {
		hc.Error = "not Open"
		return false
	}

	if value == "" {
		// 空值删除当前请求头
		hc.Request.Headers.Del(key)
	} else {
		// 设置单次请求专属请求头
		hc.Request = hc.Request.SetHeaderNonCanonical(key, value)
	}
	return true
}

// HTTPSetRequestHeaders 批量设置单次请求独立请求头
func HTTPSetRequestHeaders(ctxID int, headersText string) bool {
	hc := LoadHTTPContext(ctxID)
	if hc == nil {
		return false
	}
	hc.Lock.Lock()
	defer hc.Lock.Unlock()

	if hc.Request == nil {
		hc.Error = "not Open"
		return false
	}

	// 解析并批量绑定请求头
	headerMap := parseHeaders(headersText)
	hc.Request = hc.Request.SetHeadersNonCanonical(headerMap)
	return true
}

// ========================== 请求发送接口 ==========================

// HTTPSend 发送文本类型请求体（JSON、表单、普通文本）
// 核心优化：请求完成后自动缓存响应Body，支持多次读取
func HTTPSend(ctxID int, bodyText string) bool {
	hc := LoadHTTPContext(ctxID)
	if hc == nil {
		return false
	}
	hc.Lock.Lock()
	defer hc.Lock.Unlock()

	// 校验请求是否初始化
	if hc.Request == nil {
		hc.Error = "not Open"
		return false
	}

	// 绑定文本请求体
	if bodyText != "" {
		hc.Request = hc.Request.SetBody(bodyText)
	}

	// 发起HTTP请求
	resp, err := hc.Request.Send(hc.Method, hc.URL)
	hc.Response = resp

	// 网络请求异常处理
	if err != nil {
		hc.Error = err.Error()
		return false
	}

	// 读取并缓存响应体，解决原生Body单次读取限制
	hc.BodyBuf, err = readCloserToBytes(resp.Body)
	if err != nil {
		hc.Error = "read body error: " + err.Error()
		return false
	}
	return true
}

// HTTPSendBin 发送二进制请求体（文件、字节流、二进制数据）
func HTTPSendBin(ctxID int, bodyData []byte) bool {
	hc := LoadHTTPContext(ctxID)
	if hc == nil {
		return false
	}
	hc.Lock.Lock()
	defer hc.Lock.Unlock()

	if hc.Request == nil {
		hc.Error = "not Open"
		return false
	}

	// 绑定二进制请求体并发送
	resp, err := hc.Request.SetBody(bodyData).Send(hc.Method, hc.URL)
	hc.Response = resp

	if err != nil {
		hc.Error = err.Error()
		return false
	}

	// 缓存二进制响应体
	hc.BodyBuf, err = readCloserToBytes(resp.Body)
	if err != nil {
		hc.Error = "read body error: " + err.Error()
		return false
	}
	return true
}

// ========================== 响应数据获取接口 ==========================

// HTTPGetResponseBody 获取响应文本内容
// 返回值：响应文本、是否获取成功
func HTTPGetResponseBody(ctxID int) (string, bool) {
	hc := LoadHTTPContext(ctxID)
	if hc == nil {
		return "", false
	}
	hc.Lock.Lock()
	defer hc.Lock.Unlock()

	if hc.Response == nil {
		hc.Error = "not Open"
		return "", false
	}

	// 从缓存读取，可重复获取
	return string(hc.BodyBuf), true
}

// HTTPGetResponseBodyBin 获取响应二进制原始数据
// 适用：图片、文件、压缩包等二进制资源
func HTTPGetResponseBodyBin(ctxID int) ([]byte, bool) {
	hc := LoadHTTPContext(ctxID)
	if hc == nil {
		return nil, false
	}
	hc.Lock.Lock()
	defer hc.Lock.Unlock()

	if hc.Response == nil {
		hc.Error = "not Open"
		return nil, false
	}

	return hc.BodyBuf, true
}

// HTTPGetResponseHeaderAll 一次性取出完整头字符串 + Location，只锁一次，无并发竞争
func HTTPGetResponseHeaderAll(ctxID int) (fullHeader string, location string, ok bool) {
	defer func() { _ = recover() }()
	hc := LoadHTTPContext(ctxID)
	if hc == nil {
		return "", "", false
	}
	hc.Lock.Lock()
	defer hc.Lock.Unlock()

	if hc.Response == nil || hc.Response.Header == nil {
		hc.Error = "not Open"
		return "", "", false
	}
	fullHeader = hc.Response.HeaderToString()
	location = hc.Response.Header.Get("Location")
	// 切断底层内存引用，杜绝野指针
	fullHeader = strings.Clone(fullHeader)
	location = strings.Clone(location)
	return fullHeader, location, true
}

// HTTPGetResponseHeaders 获取完整响应头字符串（修复空指针、panic、野指针）
func HTTPGetResponseHeaders(ctxID int) (string, bool) {
	defer func() { _ = recover() }()
	hc := LoadHTTPContext(ctxID)
	if hc == nil {
		return "", false
	}
	hc.Lock.Lock()
	defer hc.Lock.Unlock()

	// 和Location接口统一判空逻辑
	if hc.Response == nil || hc.Response.Header == nil {
		hc.Error = "not Open"
		return "", false
	}

	s := hc.Response.HeaderToString()
	// 复制独立内存，防止原Header销毁后野指针
	return strings.Clone(s), true
}

func HTTPGetResponseHeader(ctxID int, key string) (string, bool) {
	defer func() {
		_ = recover()
	}()

	hc := LoadHTTPContext(ctxID)
	if hc == nil {
		return "", false
	}

	hc.Lock.Lock()
	defer hc.Lock.Unlock()

	if hc.Response == nil || hc.Response.Header == nil {
		hc.Error = "not Open"
		return "", false
	}

	// 读取并克隆，断开原Header底层内存引用，防止野指针
	val := hc.Response.Header.Get(key)
	safeVal := strings.Clone(val)

	return safeVal, true
}

// HTTPGetResponseLocation 获取响应Location头（修复recover失效问题）
func HTTPGetResponseLocation(ctxID int) (string, bool) {
	defer func() { _ = recover() }()

	hc := LoadHTTPContext(ctxID)
	if hc == nil {
		return "", false
	}
	hc.Lock.Lock()
	defer hc.Lock.Unlock()
	if hc.Response == nil || hc.Response.Header == nil {
		hc.Error = "not Open"
		return "", false
	}

	val := hc.Response.Header.Get("Location")
	return strings.Clone(val), true
}

// HTTPGetResponseStatusCode 获取HTTP状态码（200/404/500等数字码）
func HTTPGetResponseStatusCode(ctxID int) int {
	hc := LoadHTTPContext(ctxID)
	if hc == nil {
		return 0
	}
	hc.Lock.Lock()
	defer hc.Lock.Unlock()

	if hc.Response == nil {
		hc.Error = "not Open"
		return 0
	}

	return hc.Response.GetStatusCode()
}

// HTTPGetResponseStatus 获取完整状态文本（示例：200 OK、404 Not Found）
func HTTPGetResponseStatus(ctxID int) (string, bool) {
	hc := LoadHTTPContext(ctxID)
	if hc == nil {
		return "", false
	}
	hc.Lock.Lock()
	defer hc.Lock.Unlock()

	if hc.Response == nil {
		hc.Error = "not Open"
		return "", false
	}

	return hc.Response.GetStatus(), true
}

// HTTPGetError 获取当前上下文最新错误信息
func HTTPGetError(ctxID int) string {
	hc := LoadHTTPContext(ctxID)
	if hc == nil {
		return "not Init"
	}
	hc.Lock.Lock()
	defer hc.Lock.Unlock()

	return hc.Error
}

// ========================== 内部工具函数 ==========================

// parseHeaders 解析换行分隔的请求头文本为键值对Map
// 支持\r\n、\n换行格式，自动清洗首尾空格
func parseHeaders(input string) map[string]string {
	headers := make(map[string]string)
	// 按换行符分割文本
	lines := strings.FieldsFunc(input, func(r rune) bool {
		return r == '\n' || r == '\r'
	})
	// 逐行解析键值对
	for _, line := range lines {
		if idx := strings.Index(line, ":"); idx != -1 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			headers[key] = value
		}
	}
	return headers
}

// readCloserToBytes 读取IO只读流为完整字节数组
// 自动关闭流，避免资源泄露，适配HTTP响应Body流
func readCloserToBytes(rc io.ReadCloser) ([]byte, error) {
	defer rc.Close() // 函数结束自动关闭IO流
	var buf bytes.Buffer
	// 高效拷贝流数据到缓冲区
	if _, err := io.Copy(&buf, rc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
